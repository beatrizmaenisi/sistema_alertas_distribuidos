package alerta

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
	"time"
)

// Leitura é o que um sensor IoT envia. A intensidade (mm/h) é calculada na
// borda: o dispositivo soma as básculas do pluviômetro na última hora.
// Leituras são imutáveis; o estado replicado é o conjunto delas.
type Leitura struct {
	Sensor   string    `json:"id_sensor"`
	Lat      float64   `json:"lat"`
	Lng      float64   `json:"lng"`
	ChuvaMMh float64   `json:"chuva_mm_h"`
	Momento  time.Time `json:"datahora"`
}

func (l Leitura) Chave() string {
	return l.Sensor + "|" + l.Momento.UTC().Format(time.RFC3339Nano)
}

// MesclarLeitura resolve duas leituras com a mesma chave (só acontece se um
// sensor reenviar com outro conteúdo): fica o maior valor e, no empate, a de
// maior JSON, para o resultado não depender da ordem.
func MesclarLeitura(a, b Leitura) Leitura {
	if a.ChuvaMMh != b.ChuvaMMh {
		if b.ChuvaMMh > a.ChuvaMMh {
			return b
		}
		return a
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(jb) > string(ja) {
		return b
	}
	return a
}

// HashLeituras resume um conjunto de leituras, independente da ordem.
func HashLeituras(ls []Leitura) string {
	cp := append([]Leitura(nil), ls...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Chave() < cp[j].Chave() })
	for i := range cp {
		cp[i].Momento = cp[i].Momento.UTC()
	}
	b, _ := json.Marshal(cp)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:8])
}

// DistanciaKm é a distância de haversine entre dois pontos.
func DistanciaKm(lat1, lng1, lat2, lng2 float64) float64 {
	const raioTerra = 6371.0
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * raioTerra * math.Asin(math.Sqrt(h))
}
