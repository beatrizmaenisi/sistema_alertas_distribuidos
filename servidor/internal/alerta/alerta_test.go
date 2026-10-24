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
	a := Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.505, Lng: -43.175, Momento: t0})
	a.Mesclar(Novo(Relato{Dispositivo: "d1", Tipo: "ALAGAMENTO", Lat: -22.505, Lng: -43.175, Momento: t0.Add(time.Minute)}))
	if len(a.Relatos) != 1 || !a.Relatos["d1"].Momento.Equal(t0) {
		t.Fatalf("esperava 1 relato com o momento mais antigo, veio %+v", a.Relatos)
	}
}

func TestChaveMesmaCelulaEDia(t *testing.T) {
	t0 := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	k1 := ChaveDe(-22.5051, -43.1751, t0)
	k2 := ChaveDe(-22.5059, -43.1759, t0.Add(2*time.Hour))
	if k1 != k2 {
		t.Fatalf("relatos próximos no mesmo dia deveriam ter a mesma chave: %s != %s", k1, k2)
	}
	if k3 := ChaveDe(-22.52, -43.1751, t0); k3 == k1 {
		t.Fatalf("célula vizinha não deveria ter a mesma chave")
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
