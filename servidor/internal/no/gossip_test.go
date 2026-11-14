package no

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"alertas/internal/alerta"
)

func cluster(t *testing.T, tamanho int) ([]*No, []*httptest.Server) {
	t.Helper()
	nos := make([]*No, tamanho)
	srvs := make([]*httptest.Server, tamanho)
	for i := range nos {
		nos[i] = Novo(Config{ID: fmt.Sprintf("no%d", i+1)})
		srvs[i] = httptest.NewServer(nos[i].Handler())
		t.Cleanup(srvs[i].Close)
		nos[i].cfg.URL = srvs[i].URL
	}
	for i, n := range nos {
		var peers []string
		for j, s := range srvs {
			if j != i {
				peers = append(peers, s.URL)
			}
		}
		n.DefinirPeers(peers)
	}
	return nos, srvs
}

func rodadas(nos []*No, k int) {
	for r := 0; r < k; r++ {
		for _, n := range nos {
			n.rodadaGeral(context.Background())
		}
	}
}

// Relatos do mesmo evento chegam em nós diferentes: nenhum nó sozinho tem a
// corroboração, mas depois do gossip todos têm o mesmo alerta, com os dois
// usuários e a leitura do sensor, e calculam o mesmo score.
func TestCorroboracaoEntreNos(t *testing.T) {
	nos, _ := cluster(t, 3)
	t0 := time.Now().UTC()

	_, err := nos[0].AdicionarRelato(alerta.Relato{Dispositivo: "a", Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171, Momento: t0})
	if err != nil {
		t.Fatal(err)
	}
	isolado := nos[0].Alertas(time.Now())[0].Score
	nos[2].AdicionarRelato(alerta.Relato{Dispositivo: "b", Tipo: "ALAGAMENTO", Lat: -22.5054, Lng: -43.172, Momento: t0.Add(time.Minute)})
	nos[1].AdicionarLeitura(alerta.Leitura{Sensor: "s1", Lat: -22.51, Lng: -43.18, ChuvaMMh: 18, Momento: t0})

	rodadas(nos, 2)

	h := nos[0].HashEstado()
	var scores []float64
	for _, n := range nos {
		if n.HashEstado() != h {
			t.Fatalf("%s não convergiu: %s != %s", n.cfg.ID, n.HashEstado(), h)
		}
		as := n.Alertas(time.Now())
		if len(as) != 1 || as[0].NUsuarios != 2 {
			t.Fatalf("%s: esperava 1 alerta com 2 usuários, veio %+v", n.cfg.ID, as)
		}
		scores = append(scores, as[0].Score)
	}
	if scores[0] != scores[1] || scores[1] != scores[2] {
		t.Fatalf("scores diferentes entre nós: %v", scores)
	}
	if !(scores[0] > isolado) {
		t.Fatalf("corroboração deveria aumentar o score: %.3f -> %.3f", isolado, scores[0])
	}
}

// Um nó que entra vazio (ou volta de uma queda) puxa o estado dos outros.
func TestNoNovoAlcancaOsOutros(t *testing.T) {
	nos, srvs := cluster(t, 3)
	for i, n := range nos {
		n.AdicionarRelato(alerta.Relato{Dispositivo: fmt.Sprint(i), Tipo: "DESLIZAMENTO", Lat: -22.5 - float64(i)/100, Lng: -43.2, Momento: time.Now()})
	}
	rodadas(nos, 2)

	novo := Novo(Config{ID: "no4"})
	novo.DefinirPeers([]string{srvs[0].URL})
	rodadas([]*No{novo}, 1)
	if novo.HashEstado() != nos[0].HashEstado() || len(novo.Alertas(time.Now())) != 3 {
		t.Fatalf("nó novo não alcançou o estado: %d alertas", len(novo.Alertas(time.Now())))
	}
}

// Cada nó começa conhecendo só o próximo de uma fila; o gossip espalha as
// URLs e no fim todos conhecem todos, sem se conhecerem a si mesmos.
func TestDescobertaDePeers(t *testing.T) {
	nos, srvs := cluster(t, 4)
	for i, n := range nos {
		n.DefinirPeers([]string{srvs[(i+1)%len(srvs)].URL})
	}
	rodadas(nos, 3)
	for i, n := range nos {
		ps := n.listaPeers()
		if len(ps) != len(nos)-1 {
			t.Fatalf("%s conhece %d peers, esperava %d: %+v", n.cfg.ID, len(ps), len(nos)-1, ps)
		}
		for _, p := range ps {
			if p.URL == srvs[i].URL {
				t.Fatalf("%s se conhece como peer", n.cfg.ID)
			}
		}
	}
}

// Um peer fora do ar não é repassado: quem ainda não o conhecia não passa a conhecê-lo.
func TestPeerForaDoArNaoSeEspalha(t *testing.T) {
	nos, srvs := cluster(t, 3)
	nos[0].DefinirPeers([]string{srvs[1].URL, srvs[2].URL})
	nos[1].DefinirPeers([]string{srvs[0].URL})
	srvs[2].Close()
	nos[0].marcarPeer(srvs[2].URL, "", fmt.Errorf("fora do ar"))
	nos[1].rodadaGeral(context.Background())
	if ps := nos[1].listaPeers(); len(ps) != 1 {
		t.Fatalf("nos[1] não deveria ter aprendido o peer fora do ar: %+v", ps)
	}
}

func TestSensorAcimaDoLimiarDisparaAlerta(t *testing.T) {
	n := Novo(Config{ID: "no1"})
	n.AdicionarLeitura(alerta.Leitura{Sensor: "s1", Lat: -22.5, Lng: -43.2, ChuvaMMh: 10})
	if len(n.Alertas(time.Now())) != 0 {
		t.Fatal("chuva fraca não deveria disparar alerta")
	}
	n.AdicionarLeitura(alerta.Leitura{Sensor: "s1", Lat: -22.5, Lng: -43.2, ChuvaMMh: 45})
	as := n.Alertas(time.Now())
	if len(as) != 1 || len(as[0].Sensores) != 1 || as[0].NUsuarios != 0 {
		t.Fatalf("esperava 1 alerta disparado pelo sensor, veio %+v", as)
	}
}
