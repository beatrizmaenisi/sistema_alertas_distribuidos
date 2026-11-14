// Package credibilidade calcula o score de um alerta com uma regressão
// logística. Os pesos vêm de um arquivo JSON exportado pelo treinamento em
// scikit-learn (treino/exportar_modelo.py); sem arquivo, usa pesos iniciais
// definidos à mão até o modelo treinado ficar pronto.
//
// O score é derivado do estado replicado e não é trafegado no gossip: nós com
// o mesmo estado calculam o mesmo score.
package credibilidade

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"time"

	"alertas/internal/alerta"
)

const (
	// RaioSensorKm é a distância máxima entre uma estação e o centro do alerta
	// para que as leituras dela contem como corroboração.
	RaioSensorKm = 5.0
	// RaioRelatoKm é a distância até onde relatos de outros alertas também
	// contam, para que relatos dos dois lados da borda de uma célula da grade
	// se corroborem.
	RaioRelatoKm = 1.0
)

// Nomes das features. São o contrato com o script de treinamento em Python.
const (
	FeatLogNUsuarios = "log_n_usuarios" // ln(1 + usuários distintos até RaioRelatoKm)
	FeatChuvaMax     = "chuva_max_mm_h" // maior intensidade medida perto do alerta
	FeatDispersao    = "dispersao_km"   // distância média dos relatos ao centro
)

// NomesFeatures também fixa a ordem da soma, para o resultado em ponto
// flutuante ser idêntico em todos os nós.
var NomesFeatures = []string{FeatLogNUsuarios, FeatChuvaMax, FeatDispersao}

// Modelo é um Pipeline(StandardScaler, LogisticRegression) exportado.
type Modelo struct {
	Versao    string             `json:"versao"`
	Intercept float64            `json:"intercept"`
	Coef      map[string]float64 `json:"coef"`
	Media     map[string]float64 `json:"media,omitempty"`
	Escala    map[string]float64 `json:"escala,omitempty"`
}

// Padrao são pesos iniciais escolhidos à mão: um relato isolado fica abaixo
// de 0,4; vários relatos ou chuva forte medida por sensor passam de 0,7.
func Padrao() *Modelo {
	return &Modelo{
		Versao:    "heuristico-v0",
		Intercept: -2.0,
		Coef: map[string]float64{
			FeatLogNUsuarios: 1.6,
			FeatChuvaMax:     0.08,
			FeatDispersao:    -0.5,
		},
	}
}

func Carregar(caminho string) (*Modelo, error) {
	b, err := os.ReadFile(caminho)
	if err != nil {
		return nil, err
	}
	var m Modelo
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("modelo %s: %w", caminho, err)
	}
	conhecidas := map[string]bool{}
	for _, f := range NomesFeatures {
		conhecidas[f] = true
	}
	for f := range m.Coef {
		if !conhecidas[f] {
			return nil, fmt.Errorf("modelo %s: feature desconhecida %q", caminho, f)
		}
		if s, ok := m.Escala[f]; ok && s == 0 {
			return nil, fmt.Errorf("modelo %s: escala zero para %q", caminho, f)
		}
	}
	if m.Versao == "" {
		m.Versao = "sem-versao"
	}
	return &m, nil
}

type Avaliacao struct {
	Score    float64            `json:"score"`
	Nivel    string             `json:"nivel"`
	Features map[string]float64 `json:"features"`
	Versao   string             `json:"versao_modelo"`
}

// Avaliar calcula o score de a. vizinhos são os relatos de todos os alertas
// do nó (pode incluir os do próprio a).
func (m *Modelo) Avaliar(a *alerta.Alerta, vizinhos []alerta.Relato, leituras []alerta.Leitura) Avaliacao {
	f := Extrair(a, vizinhos, leituras)
	s := math.Round(m.Score(f)*1000) / 1000 // DECIMAL(4,3) no banco
	return Avaliacao{Score: s, Nivel: Nivel(s), Features: f, Versao: m.Versao}
}

func (m *Modelo) Score(f map[string]float64) float64 {
	base, contrib := m.Contribuicoes(f)
	z := base
	for _, nome := range NomesFeatures {
		z += contrib[nome]
	}
	return 1 / (1 + math.Exp(-z))
}

// Contribuicoes decompõe o logit do score: o ponto de partida (intercepto) e
// quanto cada feature soma (ou subtrai) a ele. É o que o painel mostra para
// explicar por que um alerta tem o score que tem.
func (m *Modelo) Contribuicoes(f map[string]float64) (base float64, contrib map[string]float64) {
	contrib = map[string]float64{}
	for _, nome := range NomesFeatures {
		c, ok := m.Coef[nome]
		if !ok {
			continue
		}
		x := f[nome]
		if mu, ok := m.Media[nome]; ok {
			x -= mu
		}
		if s, ok := m.Escala[nome]; ok {
			x /= s
		}
		contrib[nome] = c * x
	}
	return m.Intercept, contrib
}

// Extrair calcula as features de um alerta. Só usa o estado replicado
// (relatos e leituras), nunca o relógio local.
func Extrair(a *alerta.Alerta, vizinhos []alerta.Relato, leituras []alerta.Leitura) map[string]float64 {
	vig := a.Vigentes()
	lat, lng := a.Centro()
	// janela de corroboração: uma hora antes do primeiro relato até o fim da validade
	// (num alerta reaberto, nada de antes da reabertura conta)
	ini := a.Primeiro().Add(-time.Hour)
	if f := a.InicioFase(); ini.Before(f) {
		ini = f
	}
	fim := a.Expira()

	usuarios := map[string]bool{}
	disp := 0.0
	for _, r := range vig {
		if r.TipoAutor == alerta.Usuario {
			usuarios[r.Dispositivo] = true
		}
		disp += alerta.DistanciaKm(lat, lng, r.Lat, r.Lng)
	}
	if len(vig) > 0 {
		disp /= float64(len(vig))
	}
	for _, r := range vizinhos {
		if r.TipoAutor != alerta.Usuario || r.Momento.Before(ini) || r.Momento.After(fim) {
			continue
		}
		if alerta.DistanciaKm(lat, lng, r.Lat, r.Lng) <= RaioRelatoKm {
			usuarios[r.Dispositivo] = true
		}
	}

	chuva := 0.0
	for _, l := range leituras {
		if l.Momento.Before(ini) || l.Momento.After(fim) {
			continue
		}
		if alerta.DistanciaKm(lat, lng, l.Lat, l.Lng) > RaioSensorKm {
			continue
		}
		chuva = math.Max(chuva, l.ChuvaMMh)
	}

	return map[string]float64{
		FeatLogNUsuarios: math.Log1p(float64(len(usuarios))),
		FeatChuvaMax:     chuva,
		FeatDispersao:    disp,
	}
}

// Nivel traduz o score para os níveis do schema (alerta_nivel_enum).
func Nivel(score float64) string {
	switch {
	case score >= 0.7:
		return "EMERGENCIA"
	case score >= 0.4:
		return "ATENCAO"
	default:
		return "INFO"
	}
}
