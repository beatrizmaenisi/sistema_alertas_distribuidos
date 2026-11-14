package no

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"maps"
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

var ordemNivelPainel = map[string]int{"INFO": 0, "ATENCAO": 1, "EMERGENCIA": 2}

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
	"mistura":     mistura,
	"titulos":     titulos,
	"motivos":     motivos,
	"unicos":      unicos,
	"juntar":      strings.Join,
	"caminho":     url.PathEscape,
	"json":        func(v any) (string, error) { b, err := json.Marshal(v); return string(b), err },
	"area":        areaMapa,
	"add1":        func(n int) int { return n + 1 },
	"duracao":     duracao,
	"somar":       func(a, b float64) float64 { return a + b },
	"icone":       iconeEvento,
	"fatores":     fatores,
	"regra":       regra,
	"cartao":      func(a VisaoAlerta, agora time.Time) cartaoAlerta { return cartaoAlerta{a, agora} },
}).ParseFS(arquivosWeb, "web/index.html"))

// areaMapa é o hexágono pai, na resolução res, de onde o mapa junta o
// alerta id com os vizinhos quando está afastado.
type area struct {
	ID       string
	Contorno [][2]float64
}

func areaMapa(id string, res int) area {
	c, vs := alerta.CelulaPai(id, res)
	return area{c, vs}
}

// cartaoAlerta leva o instante da renderização junto com o alerta, para o
// template do cartão calcular "há quanto tempo" sem acessar o painel todo.
type cartaoAlerta struct {
	VisaoAlerta
	Agora time.Time
}

type linhaEvento struct {
	Momento time.Time
	Tipo    string
	Texto   string
	Tecnico string // identificadores no fim da mensagem, como "[alerta X]"
	Erro    bool
}

type painel struct {
	Agora time.Time

	No, Modelo, Hash string
	Inicio           time.Time
	Peers            []visaoPeer
	NPeersOnline     int

	Alertas     []VisaoAlerta // ativos primeiro, do mais grave para o mais leve; depois os encerrados
	NAtivos     int
	NInativos   int
	NEmergencia int

	Estacoes  []VisaoEstacao
	Chuvosas  []VisaoEstacao // as que registram chuva, da mais forte para a mais fraca
	NChuvosas int

	Eventos []linhaEvento // do mais recente para o mais antigo

	// Visão do cluster a partir deste nó: ele mesmo e os peers que conhece.
	Cluster      []noCluster
	Sincronizado bool     // todos os peers online mostram o mesmo hash deste nó
	Divergentes  []string // peers online com hash diferente (a troca ainda não terminou)
	NAlertasTot  int      // alertas replicados neste nó, ativos ou não

	// Configuração com que este nó foi iniciado.
	Intervalo string
	Fanout    int
	Limiar    float64
	Sementes  []string

	Aviso string
}

func (n *No) montarPainel() *painel {
	agora := time.Now()
	p := &painel{
		Agora: agora, No: n.cfg.ID, Modelo: n.cfg.Modelo.Versao, Inicio: n.inicio,
		Hash: n.HashEstado(), Peers: n.listaPeers(), Estacoes: n.Estacoes(),
	}

	for _, pr := range p.Peers {
		if pr.Online {
			p.NPeersOnline++
		}
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
	maisRecente := func(a, b VisaoAlerta) int { return b.Atualizado.Compare(a.Atualizado) }
	slices.SortStableFunc(p.Alertas, func(a, b VisaoAlerta) int {
		return cmp.Or(cmp.Compare(ordemNivelPainel[b.Nivel], ordemNivelPainel[a.Nivel]), maisRecente(a, b))
	})
	slices.SortStableFunc(inativos, maisRecente)
	p.NInativos = len(inativos)
	p.Alertas = append(p.Alertas, inativos...)

	for _, e := range p.Estacoes {
		if e.ChuvaMMh > 0 {
			p.Chuvosas = append(p.Chuvosas, e)
		}
	}
	slices.SortStableFunc(p.Chuvosas, func(a, b VisaoEstacao) int { return cmp.Compare(b.ChuvaMMh, a.ChuvaMMh) })
	p.NChuvosas = len(p.Chuvosas)
	p.Chuvosas = p.Chuvosas[:min(8, len(p.Chuvosas))]

	n.montarCluster(p)

	evs := n.Eventos()
	for i := len(evs) - 1; i >= 0; i-- {
		l := linhaEvento{Momento: evs[i].Momento, Tipo: evs[i].Tipo, Texto: evs[i].Texto, Erro: evs[i].Erro}
		if j := strings.LastIndex(l.Texto, " ["); j >= 0 && strings.HasSuffix(l.Texto, "]") {
			l.Texto, l.Tecnico = l.Texto[:j], l.Texto[j+2:len(l.Texto)-1]
		}
		p.Eventos = append(p.Eventos, l)
	}
	return p
}

// noCluster é um servidor no diagrama do cluster. X e Y são a posição no SVG
// (viewBox 0 0 300 190).
type noCluster struct {
	ID, URL           string
	Voce, Online      bool
	Semente           bool // veio da configuração (PEERS); senão foi descoberto pelo gossip
	Recente           bool // trocou dados há pouco: o link pisca
	Hash              string
	Igual             bool // mesmo hash que este nó
	Alertas, Estacoes int
	Inicio, Visto     time.Time
	X, Y              float64
}

func (n *No) montarCluster(p *painel) {
	d := n.digest()
	p.NAlertasTot = len(d.Alertas)
	p.Intervalo, p.Fanout, p.Limiar, p.Sementes = n.cfg.Intervalo.String(), n.cfg.Fanout, n.cfg.LimiarSensor, n.cfg.Peers
	cl := []noCluster{{ID: p.No, URL: n.cfg.URL, Voce: true, Online: true, Hash: p.Hash, Igual: true,
		Alertas: len(d.Alertas), Estacoes: len(d.Sensores), Inicio: n.inicio, Visto: p.Agora}}
	p.Sincronizado = true
	for _, pr := range p.Peers {
		c := noCluster{ID: cmp.Or(pr.ID, nomeDaURL(pr.URL)), URL: pr.URL, Online: pr.Online, Semente: pr.Semente,
			Recente: pr.Online && p.Agora.Sub(pr.UltimoOK) < 2*time.Second}
		if r := pr.Resumo; r != nil {
			c.Hash, c.Alertas, c.Estacoes, c.Inicio, c.Visto = r.Hash, r.Alertas, r.Estacoes, r.Inicio, pr.Visto
			c.Igual = r.Hash == p.Hash
		}
		if c.Online && c.Hash != "" && !c.Igual {
			p.Sincronizado = false
			p.Divergentes = append(p.Divergentes, c.ID)
		}
		cl = append(cl, c)
	}
	// você no topo, os outros espalhados numa elipse
	for i := range cl {
		ang := -math.Pi/2 + 2*math.Pi*float64(i)/float64(len(cl))
		cl[i].X, cl[i].Y = math.Round(150+105*math.Cos(ang)), math.Round(85+58*math.Sin(ang))
	}
	p.Cluster = cl
}

func (n *No) rotasPainel(mux *http.ServeMux) {
	web, _ := fs.Sub(arquivosWeb, "web")
	estaticos := http.FileServerFS(web)
	for _, rota := range []string{"/js/", "/css/", "/icones/", "/manifest.json", "/sw.js"} {
		mux.Handle("GET "+rota, estaticos)
	}

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		renderizar(w, "pagina", n.montarPainel())
	})
	mux.HandleFunc("GET /painel", func(w http.ResponseWriter, r *http.Request) {
		n.atualizacao(w, "")
	})
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

// tiposOrdenados são os tipos relatados por moradores, do mais para o menos
// relatado (empate: ordem alfabética).
func tiposOrdenados(a VisaoAlerta) []string {
	ts := slices.Collect(maps.Keys(a.Tipos))
	slices.SortFunc(ts, func(x, y string) int { return cmp.Or(cmp.Compare(a.Tipos[y], a.Tipos[x]), cmp.Compare(x, y)) })
	return ts
}

// titulos é o título do alerta: todos os tipos relatados ("Alagamento + Deslizamento"),
// para que uma área com ocorrências diferentes mostre as duas. Sem relatos de
// moradores (alerta aberto só por pluviômetro), vale o tipo predominante.
func titulos(a VisaoAlerta) string {
	ts := tiposOrdenados(a)
	if len(ts) == 0 {
		return nomeTipo(a.Tipo)
	}
	nomes := make([]string, len(ts))
	for i, t := range ts {
		nomes[i] = nomeTipo(t)
	}
	return strings.Join(nomes, " + ")
}

// mistura lista quantas pessoas relataram cada tipo. Fica vazia quando só um
// tipo foi relatado: aí o título já diz tudo.
func mistura(a VisaoAlerta) string {
	if len(a.Tipos) < 2 {
		return ""
	}
	ts := tiposOrdenados(a)
	partes := make([]string, len(ts))
	for i, t := range ts {
		partes[i] = fmt.Sprintf("%s: %d", TiposRelato[t], a.Tipos[t])
	}
	return strings.Join(partes, " · ")
}

// nomeDaURL é o nome do host de uma URL ("http://no2:8080" → "no2"), usado
// no diagrama enquanto o peer ainda não disse qual é o seu ID.
func nomeDaURL(s string) string {
	if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return s
}

// duracao é um intervalo curto e legível: "12 s", "5 min", "2 h 10 min".
func duracao(agora, t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	s := max(0, int(agora.Sub(t).Seconds()))
	switch {
	case s < 60:
		return fmt.Sprintf("%d s", s)
	case s < 3600:
		return fmt.Sprintf("%d min", s/60)
	}
	return fmt.Sprintf("%d h %d min", s/3600, s%3600/60)
}

var iconesEvento = map[string]string{
	EvRelato: "📝", EvSensor: "🌧️", EvGossip: "🔄", EvFalha: "⚠️", EvVolta: "✅", EvDescoberta: "🔎", EvStatus: "☑️",
}

func iconeEvento(tipo string) string { return cmp.Or(iconesEvento[tipo], "•") }

// fator é uma barra da explicação do score: quanto uma evidência soma (ou
// subtrai) ao logit. Largura em % da barra, de 0 a 100.
type fator struct {
	Nome, Dica string
	Valor      float64
	Largura    int
	Negativo   bool
}

var nomesFator = []struct{ chave, nome, dica string }{
	{credibilidade.FeatLogNUsuarios, "Pessoas relatando", "Quantas pessoas diferentes relataram, a até 1 km. Cada pessoa nova pesa menos que a anterior."},
	{credibilidade.FeatChuvaMax, "Chuva medida", "A maior intensidade medida pelos pluviômetros a até 5 km."},
	{credibilidade.FeatDispersao, "Relatos espalhados", "Distância média dos relatos ao centro. Quanto mais espalhados, menor a confiança."},
}

func fatores(a VisaoAlerta) []fator {
	var fs []fator
	for _, f := range nomesFator {
		v, ok := a.Contribuicoes[f.chave]
		if !ok {
			continue
		}
		fs = append(fs, fator{Nome: f.nome, Dica: f.dica, Valor: v, Negativo: v < 0,
			Largura: int(math.Min(math.Abs(v)/3, 1) * 100)})
	}
	return fs
}

// regra resume numa frase o que sustenta o score, para a plateia entender
// porque relatos sozinhos não bastam.
func regra(a VisaoAlerta) string {
	switch {
	case a.Features[credibilidade.FeatChuvaMax] > 0 && a.NUsuarios == 0:
		return "Só os pluviômetros sustentam este alerta: nenhum morador relatou ainda."
	case a.Features[credibilidade.FeatChuvaMax] > 0:
		return "Moradores e pluviômetros se confirmam: a chuva medida reforça os relatos."
	case a.NUsuarios > 1:
		return "Só relatos de moradores: sem chuva medida por perto, falta uma confirmação independente."
	}
	return "Um relato isolado pesa pouco: é preciso que outras pessoas ou pluviômetros confirmem."
}
