"""Agrupa relatos em alertas do mesmo jeito que os nós em Go (ChaveDe em
internal/alerta/alerta.go), para o treino ver os mesmos alertas que a produção.

O índice H3 é o mesmo em qualquer linguagem, então basta usar a mesma
resolução e o mesmo dia de referência (horário de Brasília).

Requer: pip install "h3>=4"

Uso:
    df["id_alerta"] = [chave_alerta(la, ln, t) for la, ln, t in
                       zip(df.lat, df.lng, df.timestamp_evento)]
"""

from datetime import datetime, timedelta, timezone

import h3

RESOLUCAO_CELULA = 8  # mesma ResolucaoCelula do Go
FUSO_REFERENCIA = timezone(timedelta(hours=-3))  # BRT, sem horário de verão


def chave_alerta(lat, lng, momento: datetime):
    """ID do alerta: célula H3 + dia em Brasília, ex. 88a8a3c0e3fffff_20220215.
    momento precisa ter fuso (datetime "aware")."""
    celula = h3.latlng_to_cell(lat, lng, RESOLUCAO_CELULA)
    return f"{celula}_{momento.astimezone(FUSO_REFERENCIA):%Y%m%d}"
