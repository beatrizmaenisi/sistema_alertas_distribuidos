package alerta

import (
	"testing"
	"time"
)

// Um sensor que reenvia a mesma leitura com outro conteúdo não pode deixar
// nós diferentes com versões diferentes.
func TestMesclarLeituraComutativa(t *testing.T) {
	m := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	a := Leitura{Sensor: "s1", Lat: -22.5, Lng: -43.2, ChuvaMMh: 10, Momento: m}
	casos := map[string]Leitura{
		"valor diferente":            {Sensor: "s1", Lat: -22.5, Lng: -43.2, ChuvaMMh: 12, Momento: m},
		"mesmo valor, outra posição": {Sensor: "s1", Lat: -22.6, Lng: -43.2, ChuvaMMh: 10, Momento: m},
	}
	for nome, b := range casos {
		if MesclarLeitura(a, b) != MesclarLeitura(b, a) {
			t.Errorf("%s: o resultado depende da ordem", nome)
		}
	}
}

func TestHashLeiturasIndependeDeOrdemEFuso(t *testing.T) {
	m := time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)
	l1 := Leitura{Sensor: "s1", ChuvaMMh: 3, Momento: m}
	l2 := Leitura{Sensor: "s1", ChuvaMMh: 5, Momento: m.Add(time.Minute)}
	l2Brasilia := l2
	l2Brasilia.Momento = l2.Momento.In(time.FixedZone("BRT", -3*60*60))
	if HashLeituras([]Leitura{l1, l2}) != HashLeituras([]Leitura{l2Brasilia, l1}) {
		t.Fatal("o hash não deveria depender da ordem nem do fuso")
	}
}
