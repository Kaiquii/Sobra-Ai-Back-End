package main

import (
	"Sobra_Ai_Back-end/internal/appversion"
	"Sobra_Ai_Back-end/internal/assistant"
	"Sobra_Ai_Back-end/internal/auth"
	"Sobra_Ai_Back-end/internal/categories"
	"Sobra_Ai_Back-end/internal/database"
	"Sobra_Ai_Back-end/internal/expenses"
	"Sobra_Ai_Back-end/internal/incomes"
	"Sobra_Ai_Back-end/internal/migrations"
	"Sobra_Ai_Back-end/internal/reports"
	"Sobra_Ai_Back-end/internal/uploads"
	"Sobra_Ai_Back-end/internal/users"
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Aviso: Arquivo .env não encontrado. Usando variáveis do sistema.")
	}

	if err := auth.ConfigureBotProtectionFromEnv(); err != nil {
		log.Fatalf("Configuracao anti-bot invalida: %v", err)
	}
	if err := auth.ConfigureEmailSenderFromEnv(); err != nil {
		log.Fatalf("Configuracao de e-mail invalida: %v", err)
	}

	database.Connect()

	if err := migrations.Validate(database.DB); err != nil {
		log.Fatalf("Banco com migrations pendentes ou incompatíveis: %v", err)
	}

	activityContext, stopActivityTracking := context.WithCancel(context.Background())
	activityTrackingDone := auth.StartUserActivityTracking(activityContext, database.DB, 30*time.Second)

	if err := os.MkdirAll(uploads.Dir(), 0755); err != nil {
		log.Fatalf("Erro ao preparar pasta de uploads: %v", err)
	}
	router := newRouter()
	log.Println("Servidor a correr na porta 8080...")
	serverErr := runServer(router)

	stopActivityTracking()
	if err := <-activityTrackingDone; err != nil {
		log.Printf("Erro ao realizar flush final de atividade: %v", err)
	}
	if serverErr != nil {
		log.Fatalf("Erro ao iniciar o servidor: %v", serverErr)
	}
}

func newRouter() *gin.Engine {
	router := gin.Default()
	config := cors.DefaultConfig()
	config.AllowOrigins = []string{"*"}
	config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization"}
	router.Use(cors.New(config))
	router.Static(uploads.PublicPath, uploads.Dir())

	api := router.Group("/api")
	{
		auth.RegisterRoutes(api)
		appversion.RegisterRoutes(api)

		protected := api.Group("/")
		protected.Use(auth.AuthMiddleware())
		{
			expenses.RegisterRoutes(protected)
			incomes.RegisterRoutes(protected)
			reports.RegisterRoutes(protected)
			categories.RegisterRoutes(protected)
			users.RegisterRoutes(protected)
			assistant.RegisterRoutes(protected)
		}
	}

	return router
}

func runServer(handler http.Handler) error {
	server := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdownSignals)

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case signalReceived := <-shutdownSignals:
		log.Printf("Sinal %s recebido; encerrando servidor com seguranca", signalReceived)
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	}
}
