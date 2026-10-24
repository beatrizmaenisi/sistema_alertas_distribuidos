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
			Lat: -22.5044, Lng: -43.171, Momento: t0.Add(time.Duration(i) * time.Minute),
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
	r1 := alerta.Relato{Dispositivo: "a", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5011, Lng: -43.175, Momento: t0}
	r2 := alerta.Relato{Dispositivo: "b", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5013, Lng: -43.175, Momento: t0}
	a1, a2 := alerta.Novo(r1), alerta.Novo(r2)
	if a1.ID == a2.ID {
		t.Fatal("o teste precisa de relatos em células diferentes")
	}
	todos := []alerta.Relato{r1, r2}
	if f := m.Avaliar(a1, todos, nil).Features[FeatLogNUsuarios]; f != math.Log1p(2) {
		t.Fatalf("esperava 2 usuários contados, feature = %.3f", f)
	}
}

func TestNivelNasFronteiras(t *testing.T) {
	casos := map[float64]string{0: "INFO", 0.399: "INFO", 0.4: "ATENCAO", 0.699: "ATENCAO", 0.7: "EMERGENCIA", 1: "EMERGENCIA"}
	for s, esperado := range casos {
		if n := Nivel(s); n != esperado {
			t.Errorf("Nivel(%v) = %s, esperado %s", s, n, esperado)
		}
	}
}

// Chuva de horas antes do primeiro relato não corrobora o alerta.
func TestLeituraForaDaJanelaNaoConta(t *testing.T) {
	a := alertaComUsuarios(1)
	velha := alerta.Leitura{Sensor: "s1", Lat: -22.51, Lng: -43.18, ChuvaMMh: 40, Momento: t0.Add(-2 * time.Hour)}
	recente := alerta.Leitura{Sensor: "s1", Lat: -22.51, Lng: -43.18, ChuvaMMh: 40, Momento: t0.Add(-30 * time.Minute)}
	if f := Extrair(a, nil, []alerta.Leitura{velha})[FeatChuvaMax]; f != 0 {
		t.Fatalf("leitura 2 h antes não deveria contar, veio %v", f)
	}
	if f := Extrair(a, nil, []alerta.Leitura{recente})[FeatChuvaMax]; f != 40 {
		t.Fatalf("leitura 30 min antes deveria contar, veio %v", f)
	}
}

func TestRelatoVizinhoAntigoNaoConta(t *testing.T) {
	m := Padrao()
	r := alerta.Relato{Dispositivo: "a", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5011, Lng: -43.175, Momento: t0}
	velho := alerta.Relato{Dispositivo: "b", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.502, Lng: -43.175, Momento: t0.Add(-3 * time.Hour)}
	if f := m.Avaliar(alerta.Novo(r), []alerta.Relato{r, velho}, nil).Features[FeatLogNUsuarios]; f != math.Log1p(1) {
		t.Fatalf("relato vizinho de 3 h antes não deveria contar, feature = %.3f", f)
	}
}

// Quem relata dos dois lados da borda de uma célula continua sendo uma pessoa.
func TestMesmoDispositivoEmDuasCelulasContaUmaVez(t *testing.T) {
	m := Padrao()
	r1 := alerta.Relato{Dispositivo: "a", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5011, Lng: -43.175, Momento: t0}
	r2 := alerta.Relato{Dispositivo: "a", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5013, Lng: -43.175, Momento: t0}
	if f := m.Avaliar(alerta.Novo(r1), []alerta.Relato{r1, r2}, nil).Features[FeatLogNUsuarios]; f != math.Log1p(1) {
		t.Fatalf("esperava 1 usuário, feature = %.3f", f)
	}
}

func TestSensorNaoContaComoUsuario(t *testing.T) {
	m := Padrao()
	r := alerta.Relato{Dispositivo: "a", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171, Momento: t0}
	s := alerta.Relato{Dispositivo: "sensor:s1", TipoAutor: alerta.Sensor, Tipo: "CHUVA_FORTE", Lat: -22.5044, Lng: -43.171, Momento: t0}
	a := alerta.Novo(r)
	a.Mesclar(alerta.Novo(s))
	if f := m.Avaliar(a, []alerta.Relato{r, s}, nil).Features[FeatLogNUsuarios]; f != math.Log1p(1) {
		t.Fatalf("o relato do sensor não deveria contar como pessoa, feature = %.3f", f)
	}
}

// Uma ocorrência que atravessa a meia-noite vira dois alertas (a chave inclui
// o dia), mas os relatos de antes da meia-noite ainda corroboram o novo.
func TestViradaDoDiaAindaCorrobora(t *testing.T) {
	m := Padrao()
	antes := alerta.Relato{Dispositivo: "a", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171,
		Momento: time.Date(2022, 2, 16, 2, 50, 0, 0, time.UTC)} // 23h50 em Brasília
	depois := alerta.Relato{Dispositivo: "b", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO", Lat: -22.5044, Lng: -43.171,
		Momento: time.Date(2022, 2, 16, 3, 10, 0, 0, time.UTC)} // 00h10 em Brasília
	a1, a2 := alerta.Novo(antes), alerta.Novo(depois)
	if a1.ID == a2.ID {
		t.Fatal("o teste precisa de alertas em dias diferentes")
	}
	if f := m.Avaliar(a2, []alerta.Relato{antes, depois}, nil).Features[FeatLogNUsuarios]; f != math.Log1p(2) {
		t.Fatalf("esperava 2 usuários depois da meia-noite, feature = %.3f", f)
	}
}

// Num alerta reaberto, os relatos e a chuva da ocorrência resolvida não
// contam: um relato novo isolado volta a ser informativo.
func TestReabertoRecomecaDoZero(t *testing.T) {
	m := Padrao()
	a := alertaComUsuarios(5)
	chuvaAntiga := alerta.Leitura{Sensor: "s1", Lat: -22.51, Lng: -43.18, ChuvaMMh: 40, Momento: t0}
	if av := m.Avaliar(a, nil, []alerta.Leitura{chuvaAntiga}); av.Nivel != "EMERGENCIA" {
		t.Fatalf("o teste precisa começar em emergência, veio %+v", av)
	}

	resolvido := t0.Add(time.Hour)
	a.Mesclar(&alerta.Alerta{ID: a.ID, Status: alerta.Resolvido, ResolvidoEm: resolvido})
	novo := alerta.Relato{Dispositivo: "outro", TipoAutor: alerta.Usuario, Tipo: "ALAGAMENTO",
		Lat: -22.5044, Lng: -43.171, Momento: resolvido.Add(alerta.Carencia + 10*time.Minute)}
	a.Mesclar(alerta.Novo(novo))

	var todos []alerta.Relato
	for _, r := range a.Relatos {
		todos = append(todos, r)
	}
	av := m.Avaliar(a, todos, []alerta.Leitura{chuvaAntiga})
	if av.Features[FeatLogNUsuarios] != math.Log1p(1) || av.Features[FeatChuvaMax] != 0 || av.Nivel != "INFO" {
		t.Fatalf("reaberto deveria contar só o relato novo, veio %+v", av)
	}
}
