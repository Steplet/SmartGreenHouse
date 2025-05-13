package services

import (
	"fmt"
	"log"

	"SmartGreenHouse/database"
	"SmartGreenHouse/models"
)

func InitScenarioService(process *models.Process, errChan chan<- string) {
	res, err := database.GetScenarios(process.Database)
	if err != nil {
		errChan <- fmt.Sprintf("Erro in InitScenario %v", err.Error())
		return
	}
	log.Println(res)
	return
}
