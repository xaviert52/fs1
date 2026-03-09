package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/rs/cors"

	"github.com/joho/godotenv"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"

	"suscriber/docs"
	"suscriber/internal/service"
)

// @title Subscription Service API
// @version 1.0
// @description Servicio de suscripción en tiempo real.
// @host localhost:8080
// @BasePath /
func main() {
	// 1. Cargar variables de entorno
	_ = godotenv.Load() // Ignoramos error si no existe el archivo (ej. Docker prod)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// 2. Detectar modo desarrollo
	devStatus := strings.TrimSpace(strings.ToLower(os.Getenv("DEVELOP_STATUS")))
	isDev := devStatus == "true" || devStatus == "1"

	if isDev {
		log.Println("⚠️  MODO DESARROLLO ACTIVADO: Usando memoria RAM (Sin Redis)")
	} else {
		log.Println("🔌  MODO PRODUCCIÓN: Conectando a Redis...")
	}

	// 3. Inicializar servicio pasando el flag
	s := service.New(isDev)

	// 4. Configurar rutas
	mux := http.NewServeMux()
	mux.Handle("/", s.Routes())
	mux.Handle("/metrics", promhttp.Handler())

	// Ruta de Swagger
	mux.Handle("/swagger/", swaggerDynamicHandler(httpSwagger.WrapHandler))

	// Configurar CORS para todos los dominios y headers
	c := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "HEAD"},
		AllowedHeaders: []string{"*"},
	})
	handler := c.Handler(mux)

	// 5. Iniciar servidor
	addr := ":" + port
	log.Printf("🚀 Servidor escuchando en http://localhost%s", addr)
	log.Printf("📄 Swagger en http://localhost%s/swagger/index.html", addr)

	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func swaggerDynamicHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/doc.json") || strings.HasSuffix(r.URL.Path, "doc.json") {
			host := r.Host
			if xfHost := r.Header.Get("X-Forwarded-Host"); xfHost != "" {
				host = xfHost
			}
			scheme := "http"
			if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
				if idx := strings.IndexByte(proto, ','); idx > 0 {
					scheme = strings.TrimSpace(proto[:idx])
				} else {
					scheme = strings.TrimSpace(proto)
				}
			} else if r.TLS != nil {
				scheme = "https"
			}

			raw := docs.SwaggerInfo.ReadDoc()
			var spec map[string]interface{}
			if err := json.Unmarshal([]byte(raw), &spec); err != nil {
				http.Error(w, "failed to load swagger doc", http.StatusInternalServerError)
				return
			}
			spec["host"] = host
			spec["schemes"] = []string{scheme}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(spec)
			return
		}
		next.ServeHTTP(w, r)
	})
}
