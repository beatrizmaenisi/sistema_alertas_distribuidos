// Package alerta define o estado que os nós replicam entre si via gossip e as
// regras de merge desse estado.
//
// O estado é um CRDT baseado em estado: as regras de merge são comutativas,
// associativas e idempotentes, então dois nós que receberam o mesmo conjunto
// de mensagens chegam ao mesmo estado, em qualquer ordem e mesmo com
// mensagens repetidas. Nada que dependa de relógio é replicado: score, nível,
// centro e expiração são derivados localmente, de forma determinística.
package alerta

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

const (
	// TamanhoCelula é o lado da célula da grade que agrupa relatos, em graus (~1,1 km).
	TamanhoCelula = 0.01
	// Validade é por quanto tempo um alerta continua ativo depois do último relato.
	Validade = 6 * time.Hour
	// Carencia é por quanto tempo, depois de resolvido, relatos novos ainda
	// são tratados como atrasados da mesma ocorrência. Um relato depois disso
	// reabre o alerta, que recomeça a credibilidade do zero.
	Carencia = 30 * time.Minute
)

// fusoReferencia define o "dia" que compõe a chave do alerta (horário de Brasília).
var fusoReferencia = time.FixedZone("BRT", -3*60*60)

type TipoAutor string

const (
	Usuario TipoAutor = "USUARIO"
	Sensor  TipoAutor = "SENSOR"
)

// Status forma um reticulado: ATIVO < RECONHECIDO < RESOLVIDO. O merge fica
// com o maior, então um alerta resolvido em qualquer nó fica resolvido em
// todos. EXPIRADO não é armazenado, é derivado da validade, e a reabertura é
// derivada de ResolvidoEm (ver Reaberto).
type Status string

const (
	Ativo       Status = "ATIVO"
	Reconhecido Status = "RECONHECIDO"
	Resolvido   Status = "RESOLVIDO"
	Expirado    Status = "EXPIRADO"
)

var ordemStatus = map[Status]int{Ativo: 0, Reconhecido: 1, Resolvido: 2}

func StatusValido(s Status) bool {
	_, ok := ordemStatus[s]
	return ok
}

// Relato é uma observação de um usuário ou de um sensor (Mensagem A).
type Relato struct {
	Dispositivo string    `json:"hash_dispositivo"`
	TipoAutor   TipoAutor `json:"tipo_autor"`
	Tipo        string    `json:"tipo"`
	Lat         float64   `json:"lat"`
	Lng         float64   `json:"lng"`
	Comentario  string    `json:"comentario,omitempty"`
	Momento     time.Time `json:"timestamp_evento"`
	NoOrigem    string    `json:"no_origem"`
}

// Alerta agrupa os relatos de uma mesma célula da grade em um mesmo dia
// (Mensagem B). Os relatos são um conjunto indexado pelo hash do dispositivo,
// então o mesmo dispositivo relatando duas vezes conta uma vez só.
type Alerta struct {
	ID      string            `json:"id_alerta"`
	Relatos map[string]Relato `json:"relatos"`
	Status  Status            `json:"status"`
	// ResolvidoEm é o momento da resolução mais recente; o merge fica com o maior.
	ResolvidoEm time.Time `json:"resolvido_em,omitzero"`
}

// ChaveDe calcula o ID do alerta a partir do local e do momento do relato.
// Como o ID é determinístico, relatos do mesmo evento recebidos por nós
// diferentes caem no mesmo alerta sem nenhuma coordenação.
func ChaveDe(lat, lng float64, t time.Time) string {
	la := int64(math.Floor(lat / TamanhoCelula))
	ln := int64(math.Floor(lng / TamanhoCelula))
	return fmt.Sprintf("%d_%d_%s", la, ln, t.In(fusoReferencia).Format("20060102"))
}

func Novo(r Relato) *Alerta {
	r.Momento = r.Momento.UTC()
	return &Alerta{
		ID:      ChaveDe(r.Lat, r.Lng, r.Momento),
		Relatos: map[string]Relato{r.Dispositivo: r},
		Status:  Ativo,
	}
}

// Mesclar incorpora o estado de b em a: união dos relatos, maior status e
// resolução mais recente.
// Se o mesmo dispositivo aparece nos dois lados, fica o relato mais antigo.
func (a *Alerta) Mesclar(b *Alerta) {
	if a.ID != b.ID {
		return
	}
	if a.Relatos == nil {
		a.Relatos = map[string]Relato{}
	}
	for k, rb := range b.Relatos {
		rb.Momento = rb.Momento.UTC()
		if ra, ok := a.Relatos[k]; !ok || relatoAntes(rb, ra) {
			a.Relatos[k] = rb
		}
	}
	if ordemStatus[b.Status] > ordemStatus[a.Status] {
		a.Status = b.Status
	}
	if b.ResolvidoEm.After(a.ResolvidoEm) {
		a.ResolvidoEm = b.ResolvidoEm.UTC()
	}
}

// relatoAntes é uma ordem total entre relatos, para o merge ser determinístico.
func relatoAntes(x, y Relato) bool {
	if !x.Momento.Equal(y.Momento) {
		return x.Momento.Before(y.Momento)
	}
	bx, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(bx) < string(by)
}

func (a *Alerta) Copia() *Alerta {
	c := &Alerta{ID: a.ID, Status: a.Status, ResolvidoEm: a.ResolvidoEm, Relatos: make(map[string]Relato, len(a.Relatos))}
	for k, r := range a.Relatos {
		c.Relatos[k] = r
	}
	return c
}

// Hash resume o estado replicado do alerta. Dois nós com o mesmo hash têm o
// mesmo alerta; é o que o gossip compara para saber o que trocar.
func (a *Alerta) Hash() string {
	b, _ := json.Marshal(a) // json ordena as chaves do mapa
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:8])
}

// Vigentes retorna os relatos dentro da validade contada a partir do mais
// recente e, se o alerta foi reaberto, posteriores à carência, ordenados por
// momento.
func (a *Alerta) Vigentes() []Relato {
	ultimo, inicio := a.Ultimo(), a.InicioFase()
	var rs []Relato
	for _, r := range a.Relatos {
		if r.Momento.After(ultimo.Add(-Validade)) && r.Momento.After(inicio) {
			rs = append(rs, r)
		}
	}
	sort.Slice(rs, func(i, j int) bool {
		if !rs[i].Momento.Equal(rs[j].Momento) {
			return rs[i].Momento.Before(rs[j].Momento)
		}
		return rs[i].Dispositivo < rs[j].Dispositivo
	})
	return rs
}

func (a *Alerta) Ultimo() time.Time {
	var t time.Time
	for _, r := range a.Relatos {
		if r.Momento.After(t) {
			t = r.Momento
		}
	}
	return t
}

func (a *Alerta) Primeiro() time.Time {
	vs := a.Vigentes()
	if len(vs) == 0 {
		return time.Time{}
	}
	return vs[0].Momento
}

func (a *Alerta) Expira() time.Time { return a.Ultimo().Add(Validade) }

// Reaberto diz se o alerta foi resolvido e depois recebeu um relato além da
// carência, ou seja, se é uma nova ocorrência no mesmo lugar e dia. Só
// depende do estado replicado, então todos os nós concordam.
func (a *Alerta) Reaberto() bool {
	return a.Status == Resolvido && !a.ResolvidoEm.IsZero() && a.Ultimo().After(a.ResolvidoEm.Add(Carencia))
}

// InicioFase é a partir de quando os relatos valem: o fim da carência, se o
// alerta foi reaberto, ou o tempo zero. Os relatos de antes descrevem a
// ocorrência já resolvida.
func (a *Alerta) InicioFase() time.Time {
	if a.Reaberto() {
		return a.ResolvidoEm.Add(Carencia)
	}
	return time.Time{}
}

func (a *Alerta) StatusEm(agora time.Time) Status {
	s := a.Status
	if a.Reaberto() {
		s = Ativo
	}
	if s != Resolvido && agora.After(a.Expira()) {
		return Expirado
	}
	return s
}

// Centro é a média das posições dos relatos vigentes.
func (a *Alerta) Centro() (lat, lng float64) {
	vs := a.Vigentes()
	if len(vs) == 0 {
		return 0, 0
	}
	for _, r := range vs {
		lat += r.Lat
		lng += r.Lng
	}
	return lat / float64(len(vs)), lng / float64(len(vs))
}

// TipoPredominante é o tipo mais relatado entre os vigentes.
func (a *Alerta) TipoPredominante() string {
	cont := map[string]int{}
	for _, r := range a.Vigentes() {
		cont[r.Tipo]++
	}
	melhor := ""
	for t, c := range cont {
		if c > cont[melhor] || (c == cont[melhor] && t < melhor) {
			melhor = t
		}
	}
	return melhor
}
