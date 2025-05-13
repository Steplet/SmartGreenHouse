package app

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"SmartGreenHouse/database"
	"SmartGreenHouse/models"
	"SmartGreenHouse/mqtt_service"
	"SmartGreenHouse/services"
	"SmartGreenHouse/web"
)

func InitProcess() {
	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan string)

	db := database.InitDB(ctx, errChan) //thread
	client := mqtt_service.InitMQTTClient(errChan)

	process := models.NewProcess(db, client, ctx)

	mqtt_service.SubscribeToDeviceTopic(process, errChan) //thread

	cronProcess := services.InitCronService(process, errChan) //thread
	services.InitScenarioService(process, errChan)

	web.RunWebServer(errChan, process, cronProcess) //thread x2

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Println("Received SIGTERM, exiting gracefully...")
	case err := <-errChan:
		time.Sleep(1 * time.Second)
		log.Println("Received error, exiting gracefully:", err)
	}
	cancel()

	time.Sleep(1 * time.Second)

}
