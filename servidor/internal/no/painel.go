package no

import (
	"bytes"
	"cmp"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"alertas/internal/alerta"
	"alertas/internal/credibilidade"
)

// O painel é renderizado no servidor com html/template (que escapa tudo) e
// atualizado pelo htmx 4. Cada atualização é uma única requisição, montada a
// partir de um único retrato do estado, que traz todos os painéis (e os dados
// do mapa) como <hx-partial>. Assim o hash, a lista de alertas e o mapa nunca
// mostram momentos diferentes do estado. Os partials usam innerMorph, que
// preserva os elementos que não mudaram: botões não somem no meio de um
// clique e a rolagem do registro não pula.

//go:embed web
var arquivosWeb embed.FS

// fusoPainel é o horário mostrado no painel (Brasília, sem horário de verão).
var fusoPainel = time.FixedZone("BRT", -3*60*60)

var nomesNivelPainel = map[string]string{"INFO": "Informativo", "ATENCAO": "Atenção", "EMERGENCIA": "Emergência"}

var orientacoes = map[string]string{
	"INFO":       "Poucos indícios até agora. Fique atento.",
	"ATENCAO":    "Indícios consistentes. Evite a área se puder.",
	"EMERGENCIA": "Forte evidência. Evite a área e siga as orientações da Defesa Civil.",
}

var modelosPainel = template.Must(template.New("").Funcs(template.FuncMap{
	"hora":        func(t time.Time) string { return t.In(fusoPainel).Format("15:04:05") },
	"haQuanto":    haQuanto,
	"num":         num,
	"pct":         pct,
	"pl":          pl,
	"relataram":   relataram,
	"intensidade": intensidade,
	"nivel":       func(n string) string { return nomesNivelPainel[n] },
	"tipo":        nomeTipo,
	"situacao":    situacao,
	"motivos":     motivos,
	"unicos":      unicos,
	"juntar":      strings.Join,
	"caminho":     url.PathEscape,
}).ParseFS(arquivosWeb, "web/index.html"))

type linhaEvento struct {
	Momento time.Time
	Texto   string
	Tecnico string // identificadores no fim da mensagem, como "[alerta X]"
	Erro    bool
}

type painel struct {
	Agora time.Time

	No, Modelo, Hash string
	Inicio           time.Time
	Peers            []visaoPeer

	Alertas     []VisaoAlerta // ativos primeiro
	NAtivos     int
	NEmergencia int

	Estacoes  []VisaoEstacao
	Chuvosas  []VisaoEstacao // as que registram chuva, da mais forte para a mais fraca
	NChuvosas int

	Eventos []linhaEvento // do mais recente para o mais antigo

	Aviso string
}

func (n *No) montarPainel() *painel {
	agora := time.Now()
	p := &painel{
		Agora: agora, No: n.cfg.ID, Modelo: n.cfg.Modelo.Versao, Inicio: n.inicio,
		Hash: n.HashEstado(), Peers: n.listaPeers(), Estacoes: n.Estacoes(),
	}

	var inativos []VisaoAlerta
	for _, a := range n.Alertas(agora) {
		if !a.Ativo() {
			inativos = append(inativos, a)
			continue
		}
		p.Alertas = append(p.Alertas, a)
		p.NAtivos++
		if a.Nivel == "EMERGENCIA" {
			p.NEmergencia++
		}
	}
	p.Alertas = append(p.Alertas, inativos...)

	for _, e := range p.Estacoes {
		if e.ChuvaMMh > 0 {
			p.Chuvosas = append(p.Chuvosas, e)
		}
	}
	slices.SortStableFunc(p.Chuvosas, func(a, b VisaoEstacao) int { return cmp.Compare(b.ChuvaMMh, a.ChuvaMMh) })
	p.NChuvosas = len(p.Chuvosas)
	p.Chuvosas = p.Chuvosas[:min(8, len(p.Chuvosas))]

	evs := n.Eventos()
	for i := len(evs) - 1; i >= 0; i-- {
		l := linhaEvento{Momento: evs[i].Momento, Texto: evs[i].Texto, Erro: evs[i].Erro}
		if j := strings.LastIndex(l.Texto, " ["); j >= 0 && strings.HasSuffix(l.Texto, "]") {
			l.Texto, l.Tecnico = l.Texto[:j], l.Texto[j+2:len(l.Texto)-1]
		}
		p.Eventos = append(p.Eventos, l)
	}
	return p
}

func (n *No) rotasPainel(mux *http.ServeMux) {
	js, _ := fs.Sub(arquivosWeb, "web/js")
	mux.Handle("GET /js/", http.StripPrefix("/js/", http.FileServerFS(js)))

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		renderizar(w, "pagina", n.montarPainel())
	})
	mux.HandleFunc("GET /painel", func(w http.ResponseWriter, r *http.Request) {
		n.atualizacao(w, "")
	})
	mux.HandleFunc("POST /painel/relatos", n.handlePainelRelato)
	mux.HandleFunc("POST /painel/alertas/{id}/resolver", func(w http.ResponseWriter, r *http.Request) {
		aviso := ""
		if err := n.MudarStatus(r.PathValue("id"), alerta.Resolvido); err != nil {
			aviso = "Não foi possível marcar como resolvido: " + err.Error()
		}
		n.atualizacao(w, aviso)
	})
}

// atualizacao responde com todos os painéis, opcionalmente com um aviso.
// Erros de uso também vão como aviso, com status 200, porque o painel só
// aplica respostas de sucesso.
func (n *No) atualizacao(w http.ResponseWriter, aviso string) {
	p := n.montarPainel()
	p.Aviso = aviso
	renderizar(w, "atualizacao", p)
}

func (n *No) handlePainelRelato(w http.ResponseWriter, r *http.Request) {
	lat, errLat := strconv.ParseFloat(r.FormValue("lat"), 64)
	lng, errLng := strconv.ParseFloat(r.FormValue("lng"), 64)
	autor := r.FormValue("autor_id")
	if errLat != nil || errLng != nil || autor == "" {
		n.atualizacao(w, "Não foi possível enviar o relato: dados incompletos.")
		return
	}
	v, err := n.AdicionarRelato(alerta.Relato{
		Dispositivo: hashDispositivo(autor),
		TipoAutor:   alerta.Usuario,
		Tipo:        r.FormValue("tipo"),
		Lat:         lat,
		Lng:         lng,
	})
	if err != nil {
		n.atualizacao(w, "Não foi possível enviar o relato: confira o tipo e o local.")
		return
	}
	n.atualizacao(w, fmt.Sprintf("Relato enviado! O alerta de %s nesta área está com credibilidade de %s (%s), porque %s na região.",
		TiposRelato[v.Tipo], pct(v.Score), nomesNivelPainel[v.Nivel], relataram(v.NUsuarios)))
}

// renderizar monta a resposta inteira antes de enviar, para um erro no
// template virar 500 em vez de uma página pela metade.
func renderizar(w http.ResponseWriter, nome string, p *painel) {
	var b bytes.Buffer
	if err := modelosPainel.ExecuteTemplate(&b, nome, p); err != nil {
		log.Printf("painel: %v", err)
		http.Error(w, "erro ao montar o painel", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b.Bytes())
}

func haQuanto(agora, t time.Time) string {
	s := max(0, int(math.Round(agora.Sub(t).Seconds())))
	switch {
	case s < 60:
		return fmt.Sprintf("há %d s", s)
	case s < 3600:
		return fmt.Sprintf("há %d min", s/60)
	}
	return fmt.Sprintf("há %d h %d min", s/3600, s%3600/60)
}

// num formata com até uma casa decimal e vírgula, como no pt-BR.
func num(x float64) string {
	return strings.Replace(strconv.FormatFloat(math.Round(x*10)/10, 'f', -1, 64), ".", ",", 1)
}

func pct(s float64) string { return strconv.Itoa(int(math.Round(s*100))) + "%" }

func pl(n int, um, varios string) string {
	if n == 1 {
		return "1 " + um
	}
	return fmt.Sprintf("%d %s", n, varios)
}

func relataram(n int) string {
	if n == 1 {
		return "1 pessoa relatou"
	}
	return fmt.Sprintf("%d pessoas diferentes relataram", n)
}

// intensidade usa a classificação usual de intensidade de chuva.
func intensidade(mmh float64) string {
	switch {
	case mmh <= 0:
		return "sem chuva"
	case mmh < 2.5:
		return "chuva fraca"
	case mmh < 10:
		return "chuva moderada"
	case mmh < 50:
		return "chuva forte"
	}
	return "chuva muito forte"
}

// nomeTipo é o nome do tipo de ocorrência com inicial maiúscula.
func nomeTipo(t string) string {
	s := TiposRelato[t]
	if s == "" {
		return t
	}
	r, tam := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[tam:]
}

func situacao(a VisaoAlerta) string {
	switch a.Status {
	case alerta.Resolvido:
		return "Encerrado: a ocorrência foi marcada como resolvida."
	case alerta.Expirado:
		return "Expirado: nenhum relato novo nas últimas 6 horas."
	case alerta.Reconhecido:
		return "Reconhecido pelas autoridades. " + orientacoes[a.Nivel]
	}
	if a.Reaberto {
		return "Reaberto: houve relatos novos depois de ter sido resolvido. " + orientacoes[a.Nivel]
	}
	return orientacoes[a.Nivel]
}

// motivos explica, em linguagem natural, as evidências que o modelo usou.
func motivos(a VisaoAlerta) []string {
	f, m := a.Features, []string{}
	if len(a.Sensores) > 0 {
		ids := make([]string, len(a.Sensores))
		for i, s := range a.Sensores {
			ids[i] = strings.TrimPrefix(s, "sensor:")
		}
		m = append(m, "Aberto automaticamente pelo pluviômetro "+strings.Join(ids, ", ")+", que mediu chuva acima do limite.")
	}
	if a.NUsuarios > 0 {
		m = append(m, relataram(a.NUsuarios)+" ocorrências num raio de 1 km.")
	} else {
		m = append(m, "Nenhum morador relatou ainda.")
	}
	if c := f[credibilidade.FeatChuvaMax]; c > 0 {
		m = append(m, fmt.Sprintf("Pluviômetros a até 5 km mediram até %s mm/h (%s).", num(c), intensidade(c)))
	} else {
		m = append(m, "Nenhum pluviômetro próximo mediu chuva, então o alerta depende só dos relatos.")
	}
	if a.NUsuarios+len(a.Sensores) > 1 {
		if d := f[credibilidade.FeatDispersao]; d < 0.3 {
			m = append(m, "Os relatos estão concentrados no mesmo ponto, o que reforça a confiança.")
		} else {
			m = append(m, fmt.Sprintf("Os relatos estão espalhados (em média %s km do centro), o que reduz a confiança.", num(d)))
		}
	}
	return m
}

func unicos(ss []string) []string {
	vistos, out := map[string]bool{}, []string{}
	for _, s := range ss {
		if !vistos[s] {
			vistos[s] = true
			out = append(out, s)
		}
	}
	return out
}
