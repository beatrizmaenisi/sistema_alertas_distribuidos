// Comando relato envia relatos de usuários simulados para um nó.
//
//	relato -no http://localhost:8081 -lat -22.505 -lng -43.175 -n 3
//
// Cada relato vem de um dispositivo diferente, espalhado em volta do ponto.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	mrand "math/rand"
	"net/http"
)

func main() {
	no := flag.String("no", "http://localhost:8081", "URL do nó")
	lat := flag.Float64("lat", -22.505, "latitude do evento")
	lng := flag.Float64("lng", -43.175, "longitude do evento")
	n := flag.Int("n", 1, "quantidade de relatos (um dispositivo por relato)")
	tipo := flag.String("tipo", "ALAGAMENTO", "ALAGAMENTO, DESLIZAMENTO, CHUVA_FORTE ou QUEDA_ARVORE")
	espalhar := flag.Float64("espalhar", 150, "raio em metros em volta do ponto")
	comentario := flag.String("comentario", "", "comentário opcional")
	flag.Parse()

	for i := 0; i < *n; i++ {
		b := make([]byte, 8)
		rand.Read(b)
		// deslocamento aleatório dentro do raio (1 grau de latitude ≈ 111 km)
		ang := mrand.Float64() * 2 * math.Pi
		d := mrand.Float64() * *espalhar / 111000
		corpo, _ := json.Marshal(map[string]any{
			"autor_id":       "sim-" + hex.EncodeToString(b),
			"tipo":           *tipo,
			"geolocalizacao": map[string]float64{"lat": *lat + d*math.Sin(ang), "lng": *lng + d*math.Cos(ang)},
			"comentario":     *comentario,
		})
		res, err := http.Post(*no+"/api/relatos", "application/json", bytes.NewReader(corpo))
		if err != nil {
			log.Fatal(err)
		}
		var v map[string]any
		json.NewDecoder(res.Body).Decode(&v)
		res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			log.Fatalf("%s: %v", res.Status, v["erro"])
		}
		fmt.Printf("relato %d → alerta %v | score %.3f | %v | %v usuário(s)\n",
			i+1, v["id_alerta"], v["score_credibilidade"], v["nivel"], v["n_usuarios"])
	}
}
