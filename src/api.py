import hashlib, hmac, json, logging, os, urllib.request
from datetime import datetime, timezone
from typing import Optional

from dotenv import load_dotenv
from fastapi import Depends, FastAPI, Header, HTTPException
from pydantic import BaseModel, Field, model_validator
from sqlalchemy import create_engine, text

load_dotenv()
engine = create_engine(os.environ["DATABASE_URL"])
API_KEY = os.environ["API_KEY"]
SALT = os.environ["SALT_DISPOSITIVO"].encode()
NOS = [u.strip().rstrip("/") for u in os.getenv("NOS_URL", "").split(",") if u.strip()]
log = logging.getLogger("uvicorn.error")

app = FastAPI(title="API de sensores")


def checar_chave(x_api_key: str = Header(default="")):
    if not hmac.compare_digest(x_api_key, API_KEY):
        raise HTTPException(status_code=401, detail="chave inválida")


class Leitura(BaseModel):
    sensor: Optional[str] = Field(default=None, max_length=32)   # ex.: "DHT22"
    dispositivo_id: str = Field(
        default_factory=lambda: os.getenv("DISPOSITIVO_PADRAO", "esp32-01"),
        min_length=1, max_length=64)
    temperatura: Optional[float] = Field(default=None, ge=-50, le=100)
    umidade: Optional[float] = Field(default=None, ge=0, le=100)
    chuva_mm_h: Optional[float] = Field(default=None, ge=0, le=500)
    medido_em: Optional[datetime] = None
    latitude: Optional[float] = Field(default=None, ge=-90, le=90)
    longitude: Optional[float] = Field(default=None, ge=-180, le=180)

    @model_validator(mode="after")
    def ao_menos_uma_medida(self):
        if self.temperatura is None and self.umidade is None and self.chuva_mm_h is None:
            raise ValueError("envie temperatura, umidade e/ou chuva_mm_h")
        return self


def _env_float(nome):
    v = os.getenv(nome)
    return float(v) if v else None


def encaminhar(l: Leitura, quando, lat, lon):
    """Repassa a leitura a um nó regional, que a replica aos demais por gossip."""
    if not NOS or lat is None or lon is None:
        return None
    corpo = {"id_sensor": l.dispositivo_id, "lat": lat, "lng": lon,
             "chuva_mm_h": l.chuva_mm_h or 0, "datahora": quando.isoformat()}
    if l.temperatura is not None:
        corpo["temperatura_c"] = l.temperatura
    if l.umidade is not None:
        corpo["umidade_pct"] = l.umidade
    for no in NOS:
        req = urllib.request.Request(
            no + "/api/leituras", data=json.dumps(corpo).encode(),
            headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=3):
                return no
        except OSError as e:
            log.warning("nó %s não recebeu a leitura: %s", no, e)
    return None


@app.get("/saude")
def saude():
    return {"ok": True}


@app.post("/leituras", status_code=201, dependencies=[Depends(checar_chave)])
def receber(l: Leitura):
    quando = l.medido_em or datetime.now(timezone.utc)
    if quando.tzinfo is None:
        quando = quando.replace(tzinfo=timezone.utc)
    h = hmac.new(SALT, l.dispositivo_id.encode(), hashlib.sha256).hexdigest()
    lat = l.latitude if l.latitude is not None else _env_float("LAT_SENSOR")
    lon = l.longitude if l.longitude is not None else _env_float("LON_SENSOR")
    with engine.begin() as con:
        r = con.execute(text("""
            INSERT INTO leituras_sensor
              (dispositivo_hash, medido_em, temperatura_c, umidade_pct,
               chuva_mm_h, latitude, longitude, modelo)
            VALUES (:h, :q, :t, :u, :c, :la, :lo, :m)
            ON CONFLICT (dispositivo_hash, medido_em) DO NOTHING
        """), dict(h=h, q=quando, t=l.temperatura, u=l.umidade, c=l.chuva_mm_h,
                   la=lat, lo=lon, m=l.sensor))
    return {"gravado": r.rowcount == 1, "encaminhado_para": encaminhar(l, quando, lat, lon)}