# sistema_alertas_distribuidos

Sistema distribuído de alertas de chuva e alagamento (vertente SmartCity, monitoramento pluviométrico urbano).

```sh
cp .env.example .env        # trocar PG_SENHA, API_KEY e SALT_DISPOSITIVO
docker compose up --build
```

| Serviço | Endereço | Pasta |
|---|---|---|
| API de ingestão dos sensores (FastAPI) | http://localhost:8000 | `src/api.py` |
| Servidores regionais e painel (Go) | http://localhost:8081 a :8084 | `servidor/` |
| PostgreSQL (banco `alertas`) | localhost:5433 | `sql/` |
| Firmware do ESP32 | | `Sensor/` |
| Treinamento do modelo de credibilidade | | `servidor/treino/` |

Caminho de uma leitura: ESP32 → `POST /leituras` na API (chave no cabeçalho `x-api-key`) → tabela `leituras_sensor` → servidor regional → demais servidores por gossip → painel.

Detalhes do servidor e roteiro de demonstração em [servidor/README.md](servidor/README.md).
