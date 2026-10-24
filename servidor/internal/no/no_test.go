package no

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"alertas/internal/alerta"
)

func TestRelatoInvalidoRecusado(t *testing.T) {
	n := Novo(Config{ID: "no1"})
	ok := alerta.Relato{Dispositivo: "a", Tipo: "ALAGAMENTO", Lat: -22.5, Lng: -43.2}
	com := func(f func(*alerta.Relato)) alerta.Relato { r := ok; f(&r); return r }
	casos := map[string]alerta.Relato{
		"sem dispositivo":   com(func(r *alerta.Relato) { r.Dispositivo = "" }),
		"tipo desconhecido": com(func(r *alerta.Relato) { r.Tipo = "TERREMOTO" }),
		"lat fora":          com(func(r *alerta.Relato) { r.Lat = 91 }),
		"lat NaN":           com(func(r *alerta.Relato) { r.Lat = math.NaN() }),
		"lng NaN":           com(func(r *alerta.Relato) { r.Lng = math.NaN() }),
		"no futuro":         com(func(r *alerta.Relato) { r.Momento = time.Now().Add(time.Hour) }),
	}
	for nome, r := range casos {
		if _, err := n.AdicionarRelato(r); err == nil {
			t.Errorf("%s: deveria ser recusado", nome)
		}
	}
	if as := n.Alertas(time.Now()); len(as) != 0 {
		t.Fatalf("nenhum relato inválido deveria virar alerta, veio %+v", as)
	}

	// relógio de celular um pouco adiantado é normal
	if _, err := n.AdicionarRelato(com(func(r *alerta.Relato) { r.Momento = time.Now().Add(time.Minute) })); err != nil {
		t.Fatalf("relato 1 min no futuro deveria ser aceito: %v", err)
	}
}

func TestLeituraInvalidaRecusada(t *testing.T) {
	n := Novo(Config{ID: "no1"})
	ok := alerta.Leitura{Sensor: "s1", Lat: -22.5, Lng: -43.2, ChuvaMMh: 5}
	com := func(f func(*alerta.Leitura)) alerta.Leitura { l := ok; f(&l); return l }
	casos := map[string]alerta.Leitura{
		"sem sensor":     com(func(l *alerta.Leitura) { l.Sensor = "" }),
		"chuva negativa": com(func(l *alerta.Leitura) { l.ChuvaMMh = -1 }),
		"chuva NaN":      com(func(l *alerta.Leitura) { l.ChuvaMMh = math.NaN() }),
		"chuva infinita": com(func(l *alerta.Leitura) { l.ChuvaMMh = math.Inf(1) }),
		"lat NaN":        com(func(l *alerta.Leitura) { l.Lat = math.NaN() }),
		"no futuro":      com(func(l *alerta.Leitura) { l.Momento = time.Now().Add(time.Hour) }),
		// ESP32 sem NTP manda a data de 1970; precisa receber erro, não 201
		"relógio zerado": com(func(l *alerta.Leitura) { l.Momento = time.Unix(10, 0) }),
	}
	for nome, l := range casos {
		if err := n.AdicionarLeitura(l); err == nil {
			t.Errorf("%s: deveria ser recusada", nome)
		}
	}
	if es := n.Estacoes(); len(es) != 0 {
		t.Fatalf("nenhuma leitura inválida deveria ser guardada, veio %+v", es)
	}
}

// Um relato mais antigo que a retenção é descartado; o nó precisa responder
// com erro e continuar funcionando.
func TestRelatoAntigoNaoTravaONo(t *testing.T) {
	n := Novo(Config{ID: "no1"})
	srv := httptest.NewServer(n.Handler())
	t.Cleanup(srv.Close)
	cli := &http.Client{Timeout: 2 * time.Second}

	corpo := `{"autor_id":"x","tipo":"ALAGAMENTO","geolocalizacao":{"lat":-22.5,"lng":-43.2},"timestamp_evento":"2020-01-01T00:00:00Z"}`
	res, err := cli.Post(srv.URL+"/api/relatos", "application/json", strings.NewReader(corpo))
	if err != nil {
		t.Fatalf("o nó deveria responder com erro, não derrubar a conexão: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("esperava 400, veio %d", res.StatusCode)
	}
	if _, err := cli.Get(srv.URL + "/api/estado"); err != nil {
		t.Fatalf("o nó travou depois do relato antigo: %v", err)
	}
}

// O formulário do painel lê lat/lng com ParseFloat, que aceita "NaN". Um
// alerta com NaN não vira JSON, e isso quebraria a API e o gossip do nó.
func TestPainelRecusaCoordenadaNaN(t *testing.T) {
	n := Novo(Config{ID: "no1"})
	srv := httptest.NewServer(n.Handler())
	t.Cleanup(srv.Close)

	res, err := http.PostForm(srv.URL+"/painel/relatos", url.Values{
		"lat": {"NaN"}, "lng": {"-43.2"}, "autor_id": {"x"}, "tipo": {"ALAGAMENTO"},
	})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if as := n.Alertas(time.Now()); len(as) != 0 {
		t.Fatalf("relato com NaN não deveria virar alerta, veio %d", len(as))
	}

	res, err = http.Get(srv.URL + "/api/estado")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var estado map[string]any
	if err := json.NewDecoder(res.Body).Decode(&estado); err != nil {
		t.Fatalf("/api/estado deveria continuar devolvendo JSON válido: %v", err)
	}
}

func TestStatusPeloNo(t *testing.T) {
	n := Novo(Config{ID: "no1"})
	v, err := n.AdicionarRelato(alerta.Relato{Dispositivo: "a", Tipo: "ALAGAMENTO", Lat: -22.5, Lng: -43.2})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.MudarStatus(v.ID, alerta.Expirado); err == nil {
		t.Error("EXPIRADO é derivado e não deveria ser aceito")
	}
	if err := n.MudarStatus("nao-existe", alerta.Resolvido); err == nil {
		t.Error("alerta inexistente deveria dar erro")
	}
	n.MudarStatus(v.ID, alerta.Resolvido)
	n.MudarStatus(v.ID, alerta.Ativo)
	if s := n.Alertas(time.Now())[0].Status; s != alerta.Resolvido {
		t.Fatalf("resolvido não pode voltar a ativo, veio %s", s)
	}
}

// Um peer com bug (ou de outra versão) manda um alerta sem status. Isso não
// pode deixar os nós com estados diferentes para sempre.
func TestGossipIgnoraStatusInvalido(t *testing.T) {
	nos, _ := cluster(t, 2)
	r := alerta.Relato{Dispositivo: "a", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171, Momento: time.Now().UTC()}
	ruim := alerta.Novo(r)
	ruim.Status = ""
	nos[0].aplicar(Pacote{De: "peer-com-bug", Alertas: []*alerta.Alerta{ruim}})
	nos[1].AdicionarRelato(alerta.Relato{Dispositivo: "b", Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171})

	rodadas(nos, 3)
	if nos[0].HashEstado() != nos[1].HashEstado() {
		t.Fatal("os nós não convergiram depois de receber um alerta sem status")
	}
	if as := nos[0].Alertas(time.Now()); len(as) != 1 || !as[0].Ativo() {
		t.Fatalf("esperava 1 alerta ativo, veio %+v", as)
	}
}

// Rede partida ao meio: cada lado recebe relatos e um lado resolve o alerta.
// Ao reconectar, todos convergem, com os relatos dos dois lados e resolvido.
func TestParticaoReconecta(t *testing.T) {
	nos, srvs := cluster(t, 4)
	for _, n := range nos {
		n.cfg.Fanout = 3 // conversa com todos os peers a cada rodada, para o teste ser determinístico
	}
	nos[0].DefinirPeers([]string{srvs[1].URL})
	nos[1].DefinirPeers([]string{srvs[0].URL})
	nos[2].DefinirPeers([]string{srvs[3].URL})
	nos[3].DefinirPeers([]string{srvs[2].URL})

	nos[0].AdicionarRelato(alerta.Relato{Dispositivo: "a", Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171})
	nos[2].AdicionarRelato(alerta.Relato{Dispositivo: "b", Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171})
	nos[3].AdicionarLeitura(alerta.Leitura{Sensor: "s1", Lat: -22.51, Lng: -43.18, ChuvaMMh: 18})
	rodadas(nos, 2)
	if nos[0].HashEstado() == nos[2].HashEstado() {
		t.Fatal("os lados da partição não deveriam ter se falado")
	}
	nos[1].MudarStatus(nos[1].Alertas(time.Now())[0].ID, alerta.Resolvido)
	rodadas(nos, 2)

	for i, n := range nos {
		var peers []string
		for j, s := range srvs {
			if j != i {
				peers = append(peers, s.URL)
			}
		}
		n.DefinirPeers(peers)
	}
	rodadas(nos, 2)

	h := nos[0].HashEstado()
	for _, n := range nos {
		if n.HashEstado() != h {
			t.Fatalf("%s não convergiu depois da partição", n.cfg.ID)
		}
		as := n.Alertas(time.Now())
		if len(as) != 1 || as[0].NUsuarios != 2 || as[0].Status != alerta.Resolvido || len(n.Estacoes()) != 1 {
			t.Fatalf("%s: esperava 1 alerta resolvido com 2 usuários e 1 estação, veio %+v", n.cfg.ID, as)
		}
	}
}

// Peer fora do ar: a rodada não pode travar, o peer aparece offline e o nó
// continua aceitando relatos.
func TestPeerForaDoAr(t *testing.T) {
	nos, srvs := cluster(t, 2)
	srvs[1].Close()

	inicio := time.Now()
	nos[0].rodadaGeral(context.Background())
	if d := time.Since(inicio); d > time.Second {
		t.Fatalf("a rodada demorou %v com o peer fora do ar", d)
	}
	if ps := nos[0].listaPeers(); ps[0].Online || ps[0].Falhas != 1 {
		t.Fatalf("o peer deveria aparecer offline com 1 falha, veio %+v", ps[0])
	}
	if _, err := nos[0].AdicionarRelato(alerta.Relato{Dispositivo: "a", Tipo: "ALAGAMENTO", Lat: -22.5, Lng: -43.2}); err != nil {
		t.Fatal(err)
	}
}

// Relatos, leituras, gossip e painel ao mesmo tempo. Rode com -race.
func TestAcessoConcorrente(t *testing.T) {
	nos, srvs := cluster(t, 3)
	ctx, cancelar := context.WithCancel(context.Background())
	var gossip, clientes sync.WaitGroup
	for _, n := range nos {
		n.cfg.Intervalo = 5 * time.Millisecond
		gossip.Add(1)
		go func() { defer gossip.Done(); n.Executar(ctx) }()
	}
	for i := 0; i < 6; i++ {
		clientes.Add(1)
		go func() {
			defer clientes.Done()
			n, srv := nos[i%3], srvs[i%3]
			for j := 0; j < 30; j++ {
				n.AdicionarRelato(alerta.Relato{Dispositivo: fmt.Sprint(i, "-", j), Tipo: "ALAGAMENTO", Lat: -22.5 - float64(j%5)/100, Lng: -43.2})
				n.AdicionarLeitura(alerta.Leitura{Sensor: fmt.Sprint("s", i), Lat: -22.5, Lng: -43.2, ChuvaMMh: float64(j)})
				if res, err := http.Get(srv.URL + "/painel"); err == nil {
					res.Body.Close()
				}
			}
		}()
	}
	clientes.Wait()
	cancelar()
	gossip.Wait()

	rodadas(nos, 2)
	for _, n := range nos {
		if n.HashEstado() != nos[0].HashEstado() {
			t.Fatalf("%s não convergiu", n.cfg.ID)
		}
	}
}

func TestRegistroDeEventosLimitado(t *testing.T) {
	n := Novo(Config{ID: "no1"})
	for i := 0; i < maxEventos+50; i++ {
		n.registrar(false, "evento %d", i)
	}
	evs := n.Eventos()
	if len(evs) != maxEventos || evs[0].Texto != "evento 50" || evs[len(evs)-1].Texto != fmt.Sprintf("evento %d", maxEventos+49) {
		t.Fatalf("esperava os %d eventos mais recentes, veio %d (de %q a %q)", maxEventos, len(evs), evs[0].Texto, evs[len(evs)-1].Texto)
	}
}
