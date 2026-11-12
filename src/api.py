import hashlib, hmac, os
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
    medido_em: Optional[datetime] = None
    latitude: Optional[float] = Field(default=None, ge=-90, le=90)
    longitude: Optional[float] = Field(default=None, ge=-180, le=180)

    @model_validator(mode="after")
    def ao_menos_uma_medida(self):
        if self.temperatura is None and self.umidade is None:
            raise ValueError("envie temperatura e/ou umidade")
        return self


def _env_float(nome):
    v = os.getenv(nome)
    return float(v) if v else None


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
               latitude, longitude, modelo)
            VALUES (:h, :q, :t, :u, :la, :lo, :m)
            ON CONFLICT (dispositivo_hash, medido_em) DO NOTHING
        """), dict(h=h, q=quando, t=l.temperatura, u=l.umidade,
                   la=lat, lo=lon, m=l.sensor))
    return {"gravado": r.rowcount == 1}