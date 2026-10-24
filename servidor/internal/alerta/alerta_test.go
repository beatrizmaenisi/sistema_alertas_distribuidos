package alerta

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func relatosAleatorios(rng *rand.Rand, n int) []Relato {
	base := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	rs := make([]Relato, n)
	for i := range rs {
		rs[i] = Relato{
			// poucos dispositivos, para forçar conflitos do mesmo dispositivo
			Dispositivo: fmt.Sprintf("d%d", rng.Intn(5)),
			TipoAutor:   Usuario,
			Tipo:        "ALAGAMENTO",
			Lat:         -22.505 + rng.Float64()*0.001,
			Lng:         -43.175 + rng.Float64()*0.001,
			Momento:     base.Add(time.Duration(rng.Intn(120)) * time.Minute),
			NoOrigem:    fmt.Sprintf("no%d", rng.Intn(3)+1),
		}
	}
	return rs
}

func alertaCom(rs []Relato, s Status) *Alerta {
	a := &Alerta{ID: "x", Relatos: map[string]Relato{}, Status: s}
	for _, r := range rs {
		a.Mesclar(&Alerta{ID: "x", Relatos: map[string]Relato{r.Dispositivo: r}, Status: Ativo})
	}
	return a
}

// O merge precisa dar o mesmo resultado em qualquer ordem e com repetições;
// é isso que garante que os nós convergem.
func TestMesclarComutativoAssociativoIdempotente(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	status := []Status{Ativo, Reconhecido, Resolvido}
	for rodada := 0; rodada < 200; rodada++ {
		partes := []*Alerta{
			alertaCom(relatosAleatorios(rng, 4), status[rng.Intn(3)]),
			alertaCom(relatosAleatorios(rng, 4), status[rng.Intn(3)]),
			alertaCom(relatosAleatorios(rng, 4), status[rng.Intn(3)]),
		}
		for _, p := range partes {
			if p.Status == Resolvido {
				p.ResolvidoEm = time.Date(2022, 2, 15, 18, rng.Intn(120), 0, 0, time.UTC)
			}
		}
		ordens := [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
		var esperado string
		for _, ordem := range ordens {
			acc := partes[ordem[0]].Copia()
			acc.Mesclar(partes[ordem[1]])
			acc.Mesclar(partes[ordem[2]])
			acc.Mesclar(partes[ordem[1]]) // repetição não pode mudar nada
			if esperado == "" {
				esperado = acc.Hash()
			} else if acc.Hash() != esperado {
				t.Fatalf("rodada %d: ordem %v deu %s, esperado %s", rodada, ordem, acc.Hash(), esperado)
			}
		}
	}
}

func TestMesmoDispositivoContaUmaVez(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	a := Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171, Momento: t0})
	a.Mesclar(Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171, Momento: t0.Add(time.Minute)}))
	if len(a.Relatos) != 1 || !a.Relatos["d1"].Momento.Equal(t0) {
		t.Fatalf("esperava 1 relato com o momento mais antigo, veio %+v", a.Relatos)
	}
}

func TestChaveMesmaCelulaEDia(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	k1 := ChaveDe(-22.5045, -43.1711, t0)
	k2 := ChaveDe(-22.5053, -43.1719, t0.Add(2*time.Hour))
	if k1 != k2 {
		t.Fatalf("relatos próximos no mesmo dia deveriam ter a mesma chave: %s != %s", k1, k2)
	}
	if k3 := ChaveDe(-22.52, -43.1751, t0); k3 == k1 {
		t.Fatalf("célula vizinha não deveria ter a mesma chave")
	}
}

// O contorno é o hexágono da chave: o centro dele cai na mesma célula.
func TestContornoEhOHexagonoDaChave(t *testing.T) {
	a := Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171, Momento: time.Now()})
	vs := a.Contorno()
	if len(vs) != 6 {
		t.Fatalf("esperava 6 vértices, veio %v", vs)
	}
	var lat, lng float64
	for _, v := range vs {
		lat += v[0] / 6
		lng += v[1] / 6
	}
	if ChaveDe(lat, lng, a.Primeiro()) != a.ID {
		t.Fatalf("o centro do contorno (%v, %v) não cai na célula do alerta %s", lat, lng, a.ID)
	}
}

func TestStatusExpira(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	a := Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.5, Lng: -43.1, Momento: t0})
	if s := a.StatusEm(t0.Add(time.Hour)); s != Ativo {
		t.Fatalf("esperava ATIVO, veio %s", s)
	}
	if s := a.StatusEm(t0.Add(Validade + time.Minute)); s != Expirado {
		t.Fatalf("esperava EXPIRADO, veio %s", s)
	}
}

// O dia da chave é o de Brasília: 22h30 em Brasília já é o dia seguinte em UTC.
func TestChaveUsaDiaDeBrasilia(t *testing.T) {
	tarde := time.Date(2022, 2, 15, 20, 0, 0, 0, time.UTC)    // 15/02 17h00 em Brasília
	noite := time.Date(2022, 2, 16, 1, 30, 0, 0, time.UTC)    // 15/02 22h30 em Brasília
	meiaNoite := time.Date(2022, 2, 16, 3, 0, 0, 0, time.UTC) // 16/02 00h00 em Brasília
	if ChaveDe(-22.5044, -43.171, tarde) != ChaveDe(-22.5044, -43.171, noite) {
		t.Fatal("15/02 à tarde e à noite em Brasília deveriam ser o mesmo alerta")
	}
	if ChaveDe(-22.5044, -43.171, noite) == ChaveDe(-22.5044, -43.171, meiaNoite) {
		t.Fatal("depois da meia-noite em Brasília deveria ser outro alerta")
	}
}

func TestStatusNaoRegride(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	a := Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.5, Lng: -43.1, Momento: t0})
	a.Mesclar(&Alerta{ID: a.ID, Status: Resolvido})
	a.Mesclar(&Alerta{ID: a.ID, Status: Ativo})
	a.Mesclar(&Alerta{ID: a.ID, Status: Reconhecido})
	if a.Status != Resolvido {
		t.Fatalf("resolvido não pode voltar atrás, veio %s", a.Status)
	}
	if s := a.StatusEm(t0.Add(48 * time.Hour)); s != Resolvido {
		t.Fatalf("resolvido não deveria virar expirado, veio %s", s)
	}
}

func TestMesclarIDDiferenteNaoFazNada(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	a := Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.5, Lng: -43.1, Momento: t0})
	antes := a.Hash()
	a.Mesclar(Novo(Relato{Dispositivo: "d2", Tipo: "ALAGAMENTO", Lat: -22.9, Lng: -43.1, Momento: t0}))
	if a.Hash() != antes {
		t.Fatal("alerta de outra célula não deveria ser mesclado")
	}

	vazio := &Alerta{ID: a.ID} // sem mapa de relatos, como chega de um JSON incompleto
	vazio.Mesclar(a)
	if len(vazio.Relatos) != 1 {
		t.Fatalf("mesclar em alerta sem mapa deveria funcionar, veio %+v", vazio.Relatos)
	}
}

// Relatos mais de 6 h antes do último continuam no conjunto (o CRDT só
// cresce), mas não entram no centro nem nas features.
func TestRelatoAntigoSaiDosVigentes(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 12, 0, 0, 0, time.UTC)
	a := Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.5045, Lng: -43.1711, Momento: t0})
	a.Mesclar(Novo(Relato{Dispositivo: "d2", Tipo: "ALAGAMENTO", Lat: -22.5053, Lng: -43.1719, Momento: t0.Add(7 * time.Hour)}))
	vs := a.Vigentes()
	if len(a.Relatos) != 2 || len(vs) != 1 || vs[0].Dispositivo != "d2" {
		t.Fatalf("esperava só d2 vigente, veio %+v", vs)
	}
	if lat, lng := a.Centro(); lat != -22.5053 || lng != -43.1719 {
		t.Fatalf("o centro deveria ignorar o relato antigo, veio %v, %v", lat, lng)
	}
}

// Todos os nós precisam mostrar o mesmo tipo, mesmo empatado (a ordem de um
// map em Go muda a cada execução).
func TestTipoPredominanteEmpateDeterministico(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	for i := 0; i < 50; i++ {
		a := Novo(Relato{Dispositivo: "d1", Tipo: "DESLIZAMENTO", Lat: -22.5, Lng: -43.1, Momento: t0})
		a.Mesclar(Novo(Relato{Dispositivo: "d2", Tipo: "ALAGAMENTO", Lat: -22.5, Lng: -43.1, Momento: t0}))
		if tp := a.TipoPredominante(); tp != "ALAGAMENTO" {
			t.Fatalf("empate deveria escolher em ordem alfabética, veio %s", tp)
		}
	}
}

func TestReaberturaDepoisDaCarencia(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 12, 0, 0, 0, time.UTC)
	resolvido := t0.Add(time.Hour)
	a := Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.5045, Lng: -43.1711, Momento: t0})
	a.Mesclar(&Alerta{ID: a.ID, Status: Resolvido, ResolvidoEm: resolvido})

	// atrasado, dentro da carência: continua resolvido
	a.Mesclar(Novo(Relato{Dispositivo: "d2", Tipo: "ALAGAMENTO", Lat: -22.5045, Lng: -43.1711, Momento: resolvido.Add(Carencia - time.Minute)}))
	if a.Reaberto() || a.StatusEm(resolvido.Add(time.Hour)) != Resolvido {
		t.Fatal("relato dentro da carência não deveria reabrir")
	}

	// depois da carência: nova ocorrência, só com os relatos novos
	novo := resolvido.Add(Carencia + time.Minute)
	a.Mesclar(Novo(Relato{Dispositivo: "d3", Tipo: "DESLIZAMENTO", Lat: -22.5053, Lng: -43.1719, Momento: novo}))
	if !a.Reaberto() || a.StatusEm(novo) != Ativo {
		t.Fatalf("relato depois da carência deveria reabrir, veio %s", a.StatusEm(novo))
	}
	if vs := a.Vigentes(); len(vs) != 1 || vs[0].Dispositivo != "d3" || a.TipoPredominante() != "DESLIZAMENTO" {
		t.Fatalf("só o relato novo deveria valer, veio %+v", vs)
	}
	if s := a.StatusEm(novo.Add(Validade + time.Minute)); s != Expirado {
		t.Fatalf("reaberto deveria expirar como um alerta comum, veio %s", s)
	}

	// resolver de novo fecha outra vez, mesmo mesclando a resolução antiga depois
	a.Mesclar(&Alerta{ID: a.ID, Status: Resolvido, ResolvidoEm: novo.Add(time.Hour)})
	a.Mesclar(&Alerta{ID: a.ID, Status: Resolvido, ResolvidoEm: resolvido})
	if a.Reaberto() || a.StatusEm(novo.Add(2*time.Hour)) != Resolvido {
		t.Fatal("a resolução mais recente deveria valer")
	}
}
