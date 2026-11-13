import json, math
from pathlib import Path
import pandas as pd

AQUI = Path(__file__).resolve().parent               
RAIZ = AQUI.parent.parent                            
m = json.load(open(RAIZ / "resultados" / "ia" / "modelo.json"))
df = pd.read_csv(RAIZ / "resultados" / "ia" / "alertas_sinteticos.csv")
amostra = df[df.conjunto == "teste"].sample(5, random_state=1)

for _, r in amostra.iterrows():
    z = m["intercept"] + sum(
        m["coef"][f] * (r[f] - m["media"][f]) / m["escala"][f] for f in m["coef"])
    p = 1 / (1 + math.exp(-z))
    nivel = "EMERGENCIA" if p >= 0.7 else "ATENCAO" if p >= 0.4 else "INFO"
    print(f'{r["tipo"]:28s} chuva={r["chuva_max_mm_h"]:5.1f} usuarios={r["n_usuarios_1km"]}'
          f' -> score={p:.2f} {nivel} (rótulo real: {r["rotulo"]})')