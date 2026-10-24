package credibilidade

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"alertas/internal/alerta"
)

var t0 = time.Date(2022, 2, 15, 18, 0, 0, 0, time.UTC)

func alertaComUsuarios(n int) *alerta.Alerta {
	var a *alerta.Alerta
	for i := 0; i < n; i++ {
		r := alerta.Relato{
			Dispositivo: fmt.Sprintf("d%d", i), TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO",
			Lat: -22.505, Lng: -43.175, Momento: t0.Add(time.Duration(i) * time.Minute),
		}
		if a == nil {
			a = alerta.Novo(r)
		} else {
			a.Mesclar(alerta.Novo(r))
		}
	}
	return a
}

func TestMaisRelatosAumentaScore(t *testing.T) {
	m := Padrao()
	s1 := m.Avaliar(alertaComUsuarios(1), nil, nil).Score
	s3 := m.Avaliar(alertaComUsuarios(3), nil, nil).Score
	if !(s3 > s1) {
		t.Fatalf("3 relatos (%.3f) deveriam valer mais que 1 (%.3f)", s3, s1)
	}
	if s1 >= 0.4 {
		t.Fatalf("relato isolado sem sensor deveria ficar abaixo de 0,4, veio %.3f", s1)
	}
}

func TestChuvaPertoCorrobora(t *testing.T) {
	m := Padrao()
	a := alertaComUsuarios(1)
	perto := alerta.Leitura{Sensor: "s1", Lat: -22.51, Lng: -43.18, ChuvaMMh: 40, Momento: t0}
	longe := alerta.Leitura{Sensor: "s2", Lat: -22.9, Lng: -43.2, ChuvaMMh: 40, Momento: t0}

	sem := m.Avaliar(a, nil, nil).Score
	comLonge := m.Avaliar(a, nil, []alerta.Leitura{longe}).Score
	comPerto := m.Avaliar(a, nil, []alerta.Leitura{perto})
	if comLonge != sem {
		t.Fatalf("estação a mais de %.0f km não deveria contar", RaioSensorKm)
	}
	if comPerto.Score < 0.7 || comPerto.Nivel != "EMERGENCIA" {
		t.Fatalf("relato + 40 mm/h perto deveria ser EMERGENCIA, veio %+v", comPerto)
	}
}

func TestCarregarModeloExportado(t *testing.T) {
	dir := t.TempDir()
	caminho := filepath.Join(dir, "modelo.json")
	json := `{"versao":"lr-teste","intercept":0.1,
		"coef":{"log_n_usuarios":1.2,"chuva_max_mm_h":0.9,"dispersao_km":-0.3},
		"media":{"log_n_usuarios":0.8,"chuva_max_mm_h":12,"dispersao_km":0.4},
		"escala":{"log_n_usuarios":0.5,"chuva_max_mm_h":15,"dispersao_km":0.3}}`
	if err := os.WriteFile(caminho, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Carregar(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if av := m.Avaliar(alertaComUsuarios(2), nil, nil); av.Versao != "lr-teste" {
		t.Fatalf("versão errada: %+v", av)
	}

	os.WriteFile(caminho, []byte(`{"coef":{"inventada":1}}`), 0o644)
	if _, err := Carregar(caminho); err == nil {
		t.Fatal("feature desconhecida deveria dar erro")
	}
}

// Relatos dos dois lados da borda de uma célula caem em alertas diferentes,
// mas devem se corroborar.
func TestRelatosVizinhosContam(t *testing.T) {
	m := Padrao()
	r1 := alerta.Relato{Dispositivo: "a", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.4999, Lng: -43.175, Momento: t0}
	r2 := alerta.Relato{Dispositivo: "b", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5001, Lng: -43.175, Momento: t0}
	a1, a2 := alerta.Novo(r1), alerta.Novo(r2)
	if a1.ID == a2.ID {
		t.Fatal("o teste precisa de relatos em células diferentes")
	}
	todos := []alerta.Relato{r1, r2}
	if f := m.Avaliar(a1, todos, nil).Features[FeatLogNUsuarios]; f != math.Log1p(2) {
		t.Fatalf("esperava 2 usuários contados, feature = %.3f", f)
	}
}
