// Package no implementa um servidor regional: recebe relatos de usuários e
// leituras de sensores, avalia a credibilidade dos alertas e replica o estado
// com os outros nós por gossip (anti-entropia push-pull).
package no

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"alertas/internal/alerta"
	"alertas/internal/credibilidade"
)

type Config struct {
	ID        string
	Peers     []string // URLs base dos outros nós
	Intervalo time.Duration
	Fanout    int // quantos peers por rodada de gossip
	Modelo    *credibilidade.Modelo
	// LimiarSensor é a intensidade (mm/h) a partir da qual uma estação gera
	// um relato sozinha, sem nenhum usuário (caso de uso "Disparar Alerta").
	LimiarSensor float64
	// Retencao é por quanto tempo leituras e alertas expirados são mantidos.
	Retencao time.Duration
}

// TiposRelato são os tipos de ocorrência aceitos (vertente SmartCity:
// monitoramento pluviométrico urbano), com o nome usado nas mensagens.
var TiposRelato = map[string]string{
	"ALAGAMENTO":   "alagamento",
	"DESLIZAMENTO": "deslizamento",
	"CHUVA_FORTE":  "chuva forte",
	"QUEDA_ARVORE": "queda de árvore",
}

var nomesNivel = map[string]string{"INFO": "informativo", "ATENCAO": "atenção", "EMERGENCIA": "emergência"}

var nomesStatus = map[alerta.Status]string{
	alerta.Ativo: "ativo", alerta.Reconhecido: "reconhecido", alerta.Resolvido: "resolvido",
}

type infoPeer struct {
	URL      string
	ID       string
	UltimoOK time.Time
	Falhas   int
}

type Evento struct {
	Momento time.Time `json:"momento"`
	Texto   string    `json:"texto"`
	Erro    bool      `json:"erro"`
}

const maxEventos = 100

type No struct {
	cfg Config

	mu       sync.Mutex
	alertas  map[string]*alerta.Alerta
	leituras map[string]alerta.Leitura
	peers    map[string]*infoPeer

	muEventos sync.Mutex
	eventos   []Evento

	gatilho chan struct{}
	cliente *http.Client
	inicio  time.Time
}

func Novo(cfg Config) *No {
	if cfg.Intervalo == 0 {
		cfg.Intervalo = time.Second
	}
	if cfg.Fanout == 0 {
		cfg.Fanout = 2
	}
	if cfg.Modelo == nil {
		cfg.Modelo = credibilidade.Padrao()
	}
	if cfg.LimiarSensor == 0 {
		cfg.LimiarSensor = 25
	}
	if cfg.Retencao == 0 {
		cfg.Retencao = 48 * time.Hour
	}
	n := &No{
		cfg:      cfg,
		alertas:  map[string]*alerta.Alerta{},
		leituras: map[string]alerta.Leitura{},
		peers:    map[string]*infoPeer{},
		gatilho:  make(chan struct{}, 1),
		cliente:  &http.Client{Timeout: 2 * time.Second},
		inicio:   time.Now(),
	}
	n.DefinirPeers(cfg.Peers)
	return n
}

func (n *No) DefinirPeers(urls []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.peers = map[string]*infoPeer{}
	for _, u := range urls {
		n.peers[u] = &infoPeer{URL: u}
	}
}

func (n *No) registrar(erro bool, formato string, args ...any) {
	e := Evento{Momento: time.Now(), Texto: fmt.Sprintf(formato, args...), Erro: erro}
	log.Printf("[%s] %s", n.cfg.ID, e.Texto)
	n.muEventos.Lock()
	defer n.muEventos.Unlock()
	n.eventos = append(n.eventos, e)
	if len(n.eventos) > maxEventos {
		n.eventos = n.eventos[len(n.eventos)-maxEventos:]
	}
}

// disparar antecipa a próxima rodada de gossip, para novidades saírem logo
// em vez de esperar o intervalo.
func (n *No) disparar() {
	select {
	case n.gatilho <- struct{}{}:
	default:
	}
}

var ErrRelatoInvalido = errors.New("relato inválido")

// ToleranciaRelogio é quanto um relato ou leitura pode vir "do futuro", para
// absorver relógios de celulares e sensores um pouco adiantados.
const ToleranciaRelogio = 5 * time.Minute

// coordenadaValida é escrita com >= e <= para recusar NaN, que falha em
// qualquer comparação (o formulário do painel aceita "NaN" via ParseFloat).
func coordenadaValida(lat, lng float64) bool {
	return lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
}

func (n *No) AdicionarRelato(r alerta.Relato) (VisaoAlerta, error) {
	if r.Dispositivo == "" || TiposRelato[r.Tipo] == "" || !coordenadaValida(r.Lat, r.Lng) {
		return VisaoAlerta{}, ErrRelatoInvalido
	}
	if r.TipoAutor == "" {
		r.TipoAutor = alerta.Usuario
	}
	if r.Momento.IsZero() {
		r.Momento = time.Now()
	}
	if r.Momento.After(time.Now().Add(ToleranciaRelogio)) {
		return VisaoAlerta{}, ErrRelatoInvalido
	}
	r.NoOrigem = n.cfg.ID

	novo := alerta.Novo(r)
	n.mu.Lock()
	n.mesclarAlerta(novo)
	a, ok := n.alertas[novo.ID]
	if !ok { // velho demais, descartado pela retenção
		n.mu.Unlock()
		return VisaoAlerta{}, ErrRelatoInvalido
	}
	v := n.visao(a, n.listaRelatos(), n.listaLeituras(), time.Now())
	n.mu.Unlock()

	n.registrar(false, "Relato de %s recebido de um usuário (%s…). Credibilidade do alerta agora: %.0f%% (%s). [alerta %s]",
		TiposRelato[r.Tipo], r.Dispositivo[:min(8, len(r.Dispositivo))], v.Score*100, nomesNivel[v.Nivel], v.ID)
	n.disparar()
	return v, nil
}

func (n *No) AdicionarLeitura(l alerta.Leitura) error {
	if l.Sensor == "" || !(l.ChuvaMMh >= 0) || math.IsInf(l.ChuvaMMh, 1) || !coordenadaValida(l.Lat, l.Lng) {
		return errors.New("leitura inválida")
	}
	agora := time.Now()
	if l.Momento.IsZero() {
		l.Momento = agora
	}
	if l.Momento.After(agora.Add(ToleranciaRelogio)) || l.Momento.Before(agora.Add(-n.cfg.Retencao)) {
		return errors.New("leitura fora da janela de tempo aceita: confira o relógio do sensor ou omita datahora")
	}
	l.Momento = l.Momento.UTC()

	n.mu.Lock()
	n.mesclarLeitura(l)
	var disparou *alerta.Alerta
	if l.ChuvaMMh >= n.cfg.LimiarSensor {
		r := alerta.Relato{
			Dispositivo: "sensor:" + l.Sensor,
			TipoAutor:   alerta.Sensor,
			Tipo:        "CHUVA_FORTE",
			Lat:         l.Lat,
			Lng:         l.Lng,
			Comentario:  fmt.Sprintf("%.1f mm/h medidos pela estação", l.ChuvaMMh),
			Momento:     l.Momento,
			NoOrigem:    n.cfg.ID,
		}
		disparou = alerta.Novo(r)
		if !n.mesclarAlerta(disparou) {
			disparou = nil // a estação já tinha disparado esse alerta
		}
	}
	n.mu.Unlock()

	if disparou != nil {
		n.registrar(false, "Pluviômetro %s mediu %.1f mm/h (limite: %.0f mm/h) e abriu sozinho um alerta de chuva forte. [alerta %s]",
			l.Sensor, l.ChuvaMMh, n.cfg.LimiarSensor, disparou.ID)
	}
	n.disparar()
	return nil
}

func (n *No) MudarStatus(id string, s alerta.Status) error {
	if !alerta.StatusValido(s) {
		return fmt.Errorf("status inválido: %s", s)
	}
	n.mu.Lock()
	a, ok := n.alertas[id]
	if ok {
		mudanca := &alerta.Alerta{ID: id, Status: s}
		if s == alerta.Resolvido {
			mudanca.ResolvidoEm = time.Now().UTC()
		}
		a.Mesclar(mudanca)
	}
	n.mu.Unlock()
	if !ok {
		return fmt.Errorf("alerta %s não encontrado", id)
	}
	n.registrar(false, "Alerta marcado como %s; a mudança será repassada aos outros servidores. [alerta %s]", nomesStatus[s], id)
	n.disparar()
	return nil
}

// mesclarAlerta incorpora b ao estado e diz se algo mudou. Precisa do lock.
func (n *No) mesclarAlerta(b *alerta.Alerta) bool {
	if b.Expira().Before(time.Now().Add(-n.cfg.Retencao)) {
		return false // velho demais, já teria sido podado
	}
	a, ok := n.alertas[b.ID]
	if !ok {
		n.alertas[b.ID] = b.Copia()
		return true
	}
	antes := a.Hash()
	a.Mesclar(b)
	return a.Hash() != antes
}

// mesclarLeitura incorpora l ao estado e diz se algo mudou. Precisa do lock.
func (n *No) mesclarLeitura(l alerta.Leitura) bool {
	if l.Momento.Before(time.Now().Add(-n.cfg.Retencao)) {
		return false
	}
	k := l.Chave()
	atual, ok := n.leituras[k]
	if !ok {
		n.leituras[k] = l
		return true
	}
	m := alerta.MesclarLeitura(atual, l)
	n.leituras[k] = m
	return m != atual
}

func (n *No) podar() {
	limite := time.Now().Add(-n.cfg.Retencao)
	n.mu.Lock()
	defer n.mu.Unlock()
	for k, l := range n.leituras {
		if l.Momento.Before(limite) {
			delete(n.leituras, k)
		}
	}
	for id, a := range n.alertas {
		if a.Expira().Before(limite) {
			delete(n.alertas, id)
		}
	}
}

func (n *No) listaRelatos() []alerta.Relato {
	var rs []alerta.Relato
	for _, a := range n.alertas {
		for _, r := range a.Relatos {
			rs = append(rs, r)
		}
	}
	return rs
}

func (n *No) listaLeituras() []alerta.Leitura {
	ls := make([]alerta.Leitura, 0, len(n.leituras))
	for _, l := range n.leituras {
		ls = append(ls, l)
	}
	return ls
}

// VisaoAlerta é o alerta como os clientes o veem: estado replicado mais os
// campos derivados (score, nível, centro, expiração).
type VisaoAlerta struct {
	ID          string             `json:"id_alerta"`
	Tipo        string             `json:"tipo"`
	Lat         float64            `json:"lat"`
	Lng         float64            `json:"lng"`
	Contorno    [][2]float64       `json:"contorno"` // vértices [lat, lng] do hexágono H3
	Status      alerta.Status      `json:"status"`
	Reaberto    bool               `json:"reaberto"`
	Nivel       string             `json:"nivel"`
	Score       float64            `json:"score_credibilidade"`
	Versao      string             `json:"versao_modelo"`
	Features    map[string]float64 `json:"features"`
	NUsuarios   int                `json:"n_usuarios"` // usuários distintos até 1 km, como o modelo conta
	Sensores    []string           `json:"sensores"`
	NosOrigem   []string           `json:"nos_origem"`
	Comentarios []string           `json:"comentarios"`
	Criado      time.Time          `json:"criado"`
	Atualizado  time.Time          `json:"atualizado_em"`
	Expira      time.Time          `json:"expira"`
	Hash        string             `json:"hash"`
}

// Ativo diz se o alerta ainda vale (não foi resolvido nem expirou).
func (v VisaoAlerta) Ativo() bool {
	return v.Status == alerta.Ativo || v.Status == alerta.Reconhecido
}

func (n *No) visao(a *alerta.Alerta, vizinhos []alerta.Relato, leituras []alerta.Leitura, agora time.Time) VisaoAlerta {
	av := n.cfg.Modelo.Avaliar(a, vizinhos, leituras)
	lat, lng := a.Centro()
	v := VisaoAlerta{
		ID: a.ID, Tipo: a.TipoPredominante(), Lat: lat, Lng: lng, Contorno: a.Contorno(),
		Status: a.StatusEm(agora), Reaberto: a.Reaberto(), Nivel: av.Nivel, Score: av.Score, Versao: av.Versao,
		Features: av.Features, Criado: a.Primeiro(), Atualizado: a.Ultimo(), Expira: a.Expira(),
		Hash: a.Hash(), Sensores: []string{}, NosOrigem: []string{}, Comentarios: []string{},
		NUsuarios: int(math.Round(math.Expm1(av.Features[credibilidade.FeatLogNUsuarios]))),
	}
	nos := map[string]bool{}
	for _, r := range a.Vigentes() {
		if r.TipoAutor == alerta.Sensor {
			v.Sensores = append(v.Sensores, r.Dispositivo)
		}
		if r.Comentario != "" {
			v.Comentarios = append(v.Comentarios, r.Comentario)
		}
		nos[r.NoOrigem] = true
	}
	for no := range nos {
		v.NosOrigem = append(v.NosOrigem, no)
	}
	sort.Strings(v.NosOrigem)
	return v
}

func (n *No) Alertas(agora time.Time) []VisaoAlerta {
	n.mu.Lock()
	defer n.mu.Unlock()
	rs, ls := n.listaRelatos(), n.listaLeituras()
	vs := make([]VisaoAlerta, 0, len(n.alertas))
	for _, a := range n.alertas {
		vs = append(vs, n.visao(a, rs, ls, agora))
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].Atualizado.After(vs[j].Atualizado) })
	return vs
}

type VisaoEstacao struct {
	Sensor   string    `json:"id_sensor"`
	Lat      float64   `json:"lat"`
	Lng      float64   `json:"lng"`
	ChuvaMMh float64   `json:"chuva_mm_h"`
	Momento  time.Time `json:"datahora"`
	Leituras int       `json:"leituras"`
}

// Estacoes retorna a leitura mais recente de cada sensor.
func (n *No) Estacoes() []VisaoEstacao {
	n.mu.Lock()
	defer n.mu.Unlock()
	por := map[string]*VisaoEstacao{}
	for _, l := range n.leituras {
		e, ok := por[l.Sensor]
		if !ok {
			e = &VisaoEstacao{Sensor: l.Sensor}
			por[l.Sensor] = e
		}
		e.Leituras++
		if l.Momento.After(e.Momento) {
			e.Lat, e.Lng, e.ChuvaMMh, e.Momento = l.Lat, l.Lng, l.ChuvaMMh, l.Momento
		}
	}
	es := make([]VisaoEstacao, 0, len(por))
	for _, e := range por {
		es = append(es, *e)
	}
	sort.Slice(es, func(i, j int) bool { return es[i].Sensor < es[j].Sensor })
	return es
}

// HashEstado resume todo o estado replicado. Nós convergidos mostram o mesmo hash.
func (n *No) HashEstado() string {
	d := n.digest()
	b, _ := json.Marshal([]map[string]string{d.Alertas, d.Sensores})
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:6])
}

func (n *No) Eventos() []Evento {
	n.muEventos.Lock()
	defer n.muEventos.Unlock()
	return append([]Evento(nil), n.eventos...)
}
