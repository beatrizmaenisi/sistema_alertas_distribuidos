
#include <DHT.h>
#include <WiFi.h>
#include <WiFiClientSecure.h>
#include <ArduinoJson.h>
#include <HTTPClient.h>
#define DHTPIN 4
#define DHTTYPE DHT22

const char* ssid = "Wifi";
const char* password = "Wifi_Senha";
const char* server = "https://ENDERECO-DA-API/leituras";
const char* apikey = "Chave";
const char* dispositivo = "esp32-01";
const unsigned long INTERVALO_MS = 10000;

DHT dht(DHTPIN, DHTTYPE);

void Wifi(){
  int passos = 0;
  Serial.print("Conectando a internet ");

  WiFi.mode(WIFI_STA);
  WiFi.begin(ssid, password);

  while(WiFi.status() != WL_CONNECTED && passos < 30){
    delay(500);
    Serial.print(".");
    passos++;
  }
  if (WiFi.status() == WL_CONNECTED){
    Serial.println("\nRede conectada.");
    Serial.print("IP: ");
    Serial.print(WiFi.localIP());
  }
  else{
    Serial.println("Não foi possivel se conectar a rede");
  }
}

void enviarDados(String jsonPayload){
  if(WiFi.status() != WL_CONNECTED){
    Serial.println("Sem Conexão, não foi possível enviar");
    return;
  }
  WiFiClientSecure seguro;
  seguro.setInsecure();
  WiFiClient simples;
  bool https = String(server).startsWith("https");

  HTTPClient http;
  if (https ? http.begin(seguro, server) : http.begin(simples, server)) {
    http.addHeader("Content-Type", "application/json");
    http.addHeader("x-api-key", apikey);

    int httpResponseCode = http.POST(jsonPayload);

    if (httpResponseCode > 0) {
      Serial.print("Enviado com sucesso! Codigo HTTP: ");
      Serial.println(httpResponseCode);
      String resposta = http.getString();
      Serial.println("Resposta do servidor: " + resposta);
    } else {
      Serial.print("Erro no envio HTTP: ");
      Serial.println(http.errorToString(httpResponseCode));
    }

    http.end();
  } else {
    Serial.println("Falha ao iniciar conexao com o servidor.");
  }
}
void setup() {
  Serial.begin(115200);
  dht.begin();
  Wifi();
}

void loop() {
  if(WiFi.status() != WL_CONNECTED){
    Wifi();
  }
  delay(INTERVALO_MS);
  float umidade = dht.readHumidity();
  float temperatura = dht.readTemperature();

  if (isnan(umidade) || isnan(temperatura)) {
    Serial.println("{\"erro\": \"Falha ao ler o sensor\"}");
    return;
  }

  JsonDocument doc;
  doc["sensor"] = "DHT22_Base";
  doc["dispositivo_id"] = dispositivo;
  doc["temperatura"] = round(temperatura * 10) / 10.0;
  doc["umidade"] = round(umidade * 10) / 10.0;

  String jsonString;
  serializeJson(doc, jsonString);
  Serial.println(jsonString);
  enviarDados(jsonString);
}
