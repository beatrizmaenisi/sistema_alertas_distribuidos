package no

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"alertas/internal/alerta"
)

func (n *No) Handler() http.Handler {
	mux := http.NewServeMux()
	n.rotasPainel(mux)
	mux.HandleFunc("GET /api/estado", n.handleEstado)
	mux.HandleFunc("GET /api/alertas", n.handleAlertas)
	mux.HandleFunc("POST /api/relatos", n.handleRelato)
	mux.HandleFunc("POST /api/leituras", n.handleLeituras)
	mux.HandleFunc("POST /api/alertas/{id}/status", n.handleStatus)
	mux.HandleFunc("POST /gossip/sync", n.handleSync)
	mux.HandleFunc("POST /gossip/push", n.handlePush)
	return mux
}

func responder(w http.ResponseWriter, codigo int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(codigo)
	json.NewEncoder(w).Encode(v)
}

func erro(w http.ResponseWriter, codigo int, msg string) {
	responder(w, codigo, map[string]string{"erro": msg})
}

func ler(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(v); err != nil {
		erro(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return false
	}
	return true
}

// entradaRelato é a Mensagem A do dicionário de dados.
type entradaRelato struct {
	AutorID        string `json:"autor_id"`
	Tipo           string `json:"tipo"`
	Geolocalizacao struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"geolocalizacao"`
	Comentario      string    `json:"comentario"`
	TimestampEvento time.Time `json:"timestamp_evento"`
}

// hashDispositivo é o identificador não reversível guardado no lugar do ID do
// aparelho (minimização de dados, LGPD).
func hashDispositivo(autor string) string {
	s := sha256.Sum256([]byte(autor))
	return hex.EncodeToString(s[:])
}

func (n *No) handleRelato(w http.ResponseWriter, r *http.Request) {
	var e entradaRelato
	if !ler(w, r, &e) {
		return
	}
	if e.AutorID == "" {
		erro(w, http.StatusBadRequest, "autor_id é obrigatório")
		return
	}
	v, err := n.AdicionarRelato(alerta.Relato{
		Dispositivo: hashDispositivo(e.AutorID),
		TipoAutor:   alerta.Usuario,
		Tipo:        e.Tipo,
		Lat:         e.Geolocalizacao.Lat,
		Lng:         e.Geolocalizacao.Lng,
		Comentario:  e.Comentario,
		Momento:     e.TimestampEvento,
	})
	if errors.Is(err, ErrRelatoInvalido) {
		erro(w, http.StatusBadRequest, "relato inválido: confira tipo e geolocalizacao")
		return
	}
	responder(w, http.StatusCreated, v)
}

// handleLeituras aceita uma leitura ou uma lista delas.
func (n *No) handleLeituras(w http.ResponseWriter, r *http.Request) {
	corpo, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
	if err != nil {
		erro(w, http.StatusBadRequest, err.Error())
		return
	}
	var ls []alerta.Leitura
	if t := bytes.TrimSpace(corpo); len(t) > 0 && t[0] == '[' {
		err = json.Unmarshal(t, &ls)
	} else {
		var l alerta.Leitura
		err = json.Unmarshal(t, &l)
		ls = append(ls, l)
	}
	if err != nil {
		erro(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	for _, l := range ls {
		if err := n.AdicionarLeitura(l); err != nil {
			erro(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	responder(w, http.StatusCreated, map[string]int{"recebidas": len(ls)})
}

// handleAlertas é o caso de uso "Consultar Alertas Ativos". Com lat, lng e
// raio_km filtra por proximidade; com todos=1 inclui expirados e resolvidos.
func (n *No) handleAlertas(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, errLat := strconv.ParseFloat(q.Get("lat"), 64)
	lng, errLng := strconv.ParseFloat(q.Get("lng"), 64)
	raio, errRaio := strconv.ParseFloat(q.Get("raio_km"), 64)
	filtrarLocal := errLat == nil && errLng == nil && errRaio == nil

	out := []VisaoAlerta{}
	for _, v := range n.Alertas(time.Now()) {
		if q.Get("todos") != "1" && !v.Ativo() {
			continue
		}
		if filtrarLocal && alerta.DistanciaKm(lat, lng, v.Lat, v.Lng) > raio {
			continue
		}
		out = append(out, v)
	}
	responder(w, http.StatusOK, out)
}

func (n *No) handleStatus(w http.ResponseWriter, r *http.Request) {
	var e struct {
		Status alerta.Status `json:"status"`
	}
	if !ler(w, r, &e) {
		return
	}
	if err := n.MudarStatus(r.PathValue("id"), e.Status); err != nil {
		erro(w, http.StatusBadRequest, err.Error())
		return
	}
	responder(w, http.StatusOK, map[string]string{"status": string(e.Status)})
}

type visaoPeer struct {
	URL      string    `json:"url"`
	ID       string    `json:"id"`
	Online   bool      `json:"online"`
	UltimoOK time.Time `json:"ultimo_ok"`
	Falhas   int       `json:"falhas_seguidas"`
}

func (n *No) listaPeers() []visaoPeer {
	n.mu.Lock()
	peers := make([]visaoPeer, 0, len(n.peers))
	for _, p := range n.peers {
		peers = append(peers, visaoPeer{
			URL: p.URL, ID: p.ID, UltimoOK: p.UltimoOK, Falhas: p.Falhas,
			Online: !p.UltimoOK.IsZero() && p.Falhas == 0,
		})
	}
	n.mu.Unlock()
	sort.Slice(peers, func(i, j int) bool { return peers[i].URL < peers[j].URL })
	return peers
}

func (n *No) handleEstado(w http.ResponseWriter, r *http.Request) {
	responder(w, http.StatusOK, map[string]any{
		"no":            n.cfg.ID,
		"inicio":        n.inicio,
		"versao_modelo": n.cfg.Modelo.Versao,
		"hash_estado":   n.HashEstado(),
		"peers":         n.listaPeers(),
		"alertas":       n.Alertas(time.Now()),
		"estacoes":      n.Estacoes(),
		"eventos":       n.Eventos(),
	})
}

func (n *No) handleSync(w http.ResponseWriter, r *http.Request) {
	var d Digest
	if !ler(w, r, &d) {
		return
	}
	responder(w, http.StatusOK, n.responderSync(d))
}

func (n *No) handlePush(w http.ResponseWriter, r *http.Request) {
	var p Pacote
	if !ler(w, r, &p) {
		return
	}
	a, l := n.aplicar(p)
	if a+l > 0 {
		n.registrar(false, "Recebeu novidades de %s (gossip): %s.", p.De, contagem(a, l))
	}
	responder(w, http.StatusOK, map[string]int{"alertas": a, "leituras": l})
}
