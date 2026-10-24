# Alertas Distribuídos — servidor (Go)

**Vertente: SmartCity — monitoramento pluviométrico urbano.**

Servidores regionais que recebem relatos de cidadãos e leituras de
pluviômetros IoT, calculam a credibilidade de cada alerta e replicam o estado
entre si por protocolo gossip, sem ponto único de falha.

## Como rodar

Com Docker (é o que vai no manual):

```sh
docker compose up --build        # sobe no1, no2, no3
# painéis: http://localhost:8081  http://localhost:8082  http://localhost:8083
docker compose stop no2          # derruba um nó
docker compose start no2         # volta e alcança os outros sozinho
```

Sem Docker (Go 1.24+):

```sh
./scripts/cluster.sh             # sobe os 3 nós; Ctrl+C derruba todos
kill $(cat /tmp/alertas-no2.pid) # derruba só o no2
go test ./...                    # testes
```

Ferramentas de simulação:

```sh
go build -o bin/ ./cmd/...

# reproduz o evento de Petrópolis (15/02/2022) com dados reais do CEMADEN, 60x mais rápido
./bin/replay -velocidade 60

# relatos de usuários simulados (cada um de um dispositivo diferente)
./bin/relato -no http://localhost:8081 -lat -22.505 -lng -43.18 -n 3 -comentario "rua alagada"
```

## Roteiro da demo

1. Abrir os 3 painéis lado a lado. Mostrar que os três têm o mesmo **hash de estado** no topo.
2. `./bin/replay -velocidade 120` — leituras reais chegam em nós diferentes; estações acima
   de 25 mm/h disparam alertas sozinhas (sensor como nó âncora). Os três painéis mostram os mesmos alertas.
3. Clicar no mapa do **no1** numa área sem estação → alerta INFO (~0,29). Clicar perto do mesmo
   ponto no painel do **no3** → o score sobe nos três nós (corroboração entre servidores).
4. `./bin/relato -no http://localhost:8082 -lat -22.9068 -lng -43.1729 -n 5` — 5 relatos falsos
   num lugar seco: sobe, mas fica em ATENCAO; sem sensor não chega a EMERGENCIA.
5. `docker compose stop no2` → no2 aparece OFFLINE nos outros; continuar relatando no no1.
   `docker compose start no2` → ele volta vazio e em ~1 s puxa todo o estado (hash igual de novo).

## Arquitetura

```
cmd/no            servidor regional (API HTTP + gossip + painel)
cmd/replay        replay de CSV do CEMADEN como sensores IoT
cmd/relato        gerador de relatos de usuários
internal/alerta   estado replicado e regras de merge (CRDT)
internal/credibilidade   features + regressão logística
internal/no       nó: estado, gossip, API, painel (web/index.html)
treino/           exportação do modelo do scikit-learn para JSON
```

### Estado replicado

- **ID do alerta** = célula da grade de 0,01° (~1,1 km) + dia (horário de Brasília).
  Relatos do mesmo evento recebidos por nós diferentes caem no mesmo alerta sem coordenação.
- **Relatos**: conjunto indexado pelo hash SHA-256 do dispositivo (LGPD). Merge = união;
  o mesmo dispositivo conta uma vez só.
- **Leituras**: conjunto indexado por (sensor, datahora). Merge = união.
- **Status**: ATIVO < RECONHECIDO < RESOLVIDO. Merge = maior. EXPIRADO é derivado (6 h sem relato novo).
- **Score, nível, centro e expiração não trafegam**: cada nó calcula a partir do estado.
  Nós com o mesmo estado calculam o mesmo score.

Como o merge é comutativo, associativo e idempotente, os nós convergem em qualquer ordem
de mensagens e com mensagens repetidas (testado em `internal/alerta/alerta_test.go`). Isso
trata a limitação "nós podem possuir valores incongruentes para o mesmo evento" do pré-projeto.

### Gossip (anti-entropia push-pull, Demers et al. 1987)

A cada 1 s (ou logo após uma novidade), cada nó escolhe `fanout` peers e:

1. envia um **digest** (hash de cada alerta e do conjunto de leituras de cada sensor);
2. o peer responde com o que o primeiro não tem ou tem diferente, e pede o que falta para ele;
3. o primeiro mescla e envia o que foi pedido.

No fim de uma rodada os dois têm o mesmo estado. Um nó que volta de uma queda puxa tudo na primeira rodada.

### Credibilidade

Regressão logística sobre três features (definições em `internal/credibilidade` e `treino/exportar_modelo.py`):

| feature | significado |
|---|---|
| `log_n_usuarios` | ln(1 + usuários distintos que relataram a até 1 km) |
| `chuva_max_mm_h` | maior intensidade medida por estação a até 5 km, de 1 h antes do 1º relato até a expiração |
| `dispersao_km` | distância média dos relatos ao centro |

Hoje roda com pesos iniciais (`heuristico-v0`). O modelo treinado entra por arquivo:
treinar `Pipeline(StandardScaler, LogisticRegression)` no scikit-learn, exportar com
`treino/exportar_modelo.py` e iniciar os nós com `-modelo modelo/modelo.json` (ou `MODELO=` no compose).
Score ≥ 0,7 → EMERGENCIA, ≥ 0,4 → ATENCAO, senão INFO.

## API

| método | rota | uso |
|---|---|---|
| POST | `/api/relatos` | relato de usuário (Mensagem A) |
| POST | `/api/leituras` | leitura de sensor (uma ou lista) — é o que o ESP32 chama |
| GET | `/api/alertas?lat=&lng=&raio_km=` | alertas ativos perto de um ponto (`todos=1` inclui expirados) |
| POST | `/api/alertas/{id}/status` | `{"status":"RESOLVIDO"}` |
| GET | `/api/estado` | tudo o que o painel mostra |
| POST | `/gossip/sync`, `/gossip/push` | entre nós |

Relato:

```json
{"autor_id": "id-do-aparelho", "tipo": "ALAGAMENTO",
 "geolocalizacao": {"lat": -22.505, "lng": -43.18},
 "comentario": "rua alagada", "timestamp_evento": "2026-10-27T14:00:00-03:00"}
```

`tipo`: ALAGAMENTO, DESLIZAMENTO, CHUVA_FORTE, QUEDA_ARVORE. `timestamp_evento` é opcional.

Leitura (firmware do ESP32 calcula a intensidade na borda somando as básculas da última hora):

```json
{"id_sensor": "esp32-01", "lat": -22.505, "lng": -43.18, "chuva_mm_h": 12.4}
```

`datahora` é opcional (sem NTP o servidor usa a hora de chegada).

## Limitações conhecidas

- Estado só em memória: um nó que reinicia volta vazio e recupera pelo gossip; se **todos**
  caírem, perde-se tudo. Persistência em PostgreSQL é o próximo passo (`../sistema_alertas_distribuidos/sql/schema.sql`).
- Relatos dos dois lados da borda de uma célula viram dois alertas no mapa (ambos com o score
  certo, porque a contagem de usuários olha 1 km em volta).
- Lista de peers estática; sem autenticação entre nós nem de sensores.
- Dashboard carrega Leaflet e o mapa da internet.
