// Comando replay reproduz leituras reais do CEMADEN como se fossem sensores
// IoT enviando para os nós, com o tempo acelerado.
//
// Para cada leitura ele faz o que o firmware do ESP32 fará na borda: soma a
// chuva da última hora daquela estação e envia a intensidade em mm/h. Cada
// estação é atribuída sempre ao mesmo nó, como um sensor ligado ao servidor
// regional mais próximo.
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"alertas/internal/alerta"
)

const layoutCEMADEN = "2006-01-02 15:04:05" // aceita o ".0" do arquivo

type registro struct {
	cod, nome string
	lat, lng  float64
	momento   time.Time // UTC, como o CEMADEN informa
	mm        float64   // chuva acumulada desde a leitura anterior
}

func numero(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
}

func carregar(caminho string) ([]registro, error) {
	f, err := os.Open(caminho)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = ';'
	linhas, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, c := range linhas[0] {
		col[strings.TrimPrefix(c, "\uFEFF")] = i
	}
	for _, c := range []string{"codEstacao", "nomeEstacao", "latitude", "longitude", "datahora", "valorMedida"} {
		if _, ok := col[c]; !ok {
			return nil, fmt.Errorf("%s: coluna %s ausente", caminho, c)
		}
	}
	var rs []registro
	for _, l := range linhas[1:] {
		lat, e1 := numero(l[col["latitude"]])
		lng, e2 := numero(l[col["longitude"]])
		mm, e3 := numero(l[col["valorMedida"]])
		t, e4 := time.Parse(layoutCEMADEN, l[col["datahora"]])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			continue
		}
		rs = append(rs, registro{l[col["codEstacao"]], l[col["nomeEstacao"]], lat, lng, t, mm})
	}
	return rs, nil
}

func noDaEstacao(cod string, nos []string) string {
	h := fnv.New32a()
	h.Write([]byte(cod))
	return nos[h.Sum32()%uint32(len(nos))]
}

func main() {
	arquivo := flag.String("arquivo", "../dados/raw/RJ_Petropolis_2022-02.csv", "CSV do CEMADEN")
	inicioStr := flag.String("inicio", "2022-02-15 17:00", "início do trecho, em UTC")
	fimStr := flag.String("fim", "2022-02-16 00:00", "fim do trecho, em UTC")
	nosStr := flag.String("nos", "http://localhost:8081,http://localhost:8082,http://localhost:8083", "URLs dos nós")
	velocidade := flag.Float64("velocidade", 60, "aceleração do tempo (60 = 1 h em 1 min)")
	flag.Parse()

	inicio, err := time.Parse("2006-01-02 15:04", *inicioStr)
	if err != nil {
		log.Fatal(err)
	}
	fim, err := time.Parse("2006-01-02 15:04", *fimStr)
	if err != nil {
		log.Fatal(err)
	}
	nos := strings.Split(*nosStr, ",")

	rs, err := carregar(*arquivo)
	if err != nil {
		log.Fatal(err)
	}
	sort.Slice(rs, func(i, j int) bool { return rs[i].momento.Before(rs[j].momento) })

	// histórico por estação, para a soma móvel de 1 h
	porEstacao := map[string][]registro{}
	for _, r := range rs {
		porEstacao[r.cod] = append(porEstacao[r.cod], r)
	}
	chuvaUltimaHora := func(r registro) float64 {
		soma := 0.0
		for _, h := range porEstacao[r.cod] {
			if h.momento.After(r.momento.Add(-time.Hour)) && !h.momento.After(r.momento) {
				soma += h.mm
			}
		}
		return soma
	}

	var trecho []registro
	for _, r := range rs {
		if !r.momento.Before(inicio) && r.momento.Before(fim) {
			trecho = append(trecho, r)
		}
	}
	if len(trecho) == 0 {
		log.Fatalf("nenhuma leitura entre %s e %s", inicio, fim)
	}
	duracao := time.Duration(float64(fim.Sub(inicio)) / *velocidade)
	log.Printf("%d leituras de %d estações, %s a %s UTC, em %s", len(trecho), len(porEstacao),
		inicio.Format("02/01 15:04"), fim.Format("02/01 15:04"), duracao.Round(time.Second))

	cliente := &http.Client{Timeout: 2 * time.Second}
	partida := time.Now()
	for _, r := range trecho {
		alvo := partida.Add(time.Duration(float64(r.momento.Sub(inicio)) / *velocidade))
		time.Sleep(time.Until(alvo))

		// o momento enviado é o de agora: o sensor carimba a leitura ao enviar
		l := alerta.Leitura{Sensor: r.cod, Lat: r.lat, Lng: r.lng, ChuvaMMh: chuvaUltimaHora(r), Momento: time.Now().UTC()}
		corpo, _ := json.Marshal(l)
		no := noDaEstacao(r.cod, nos)
		res, err := cliente.Post(no+"/api/leituras", "application/json", bytes.NewReader(corpo))
		status := "ok"
		if err != nil {
			status = "FALHOU"
		} else {
			res.Body.Close()
			if res.StatusCode != http.StatusCreated {
				status = res.Status
			}
		}
		if l.ChuvaMMh > 0 || status != "ok" {
			fmt.Printf("[%s UTC] %-26s → %s  %5.1f mm/h  %s\n",
				r.momento.Format("02/01 15:04"), r.nome, no, l.ChuvaMMh, status)
		}
	}
	log.Print("replay concluído")
}
