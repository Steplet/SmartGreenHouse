package services

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"SmartGreenHouse/database"
	"SmartGreenHouse/models"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/robfig/cron/v3"
)

const layout = "2006-01-02T15:04"

var (
	ScheduleMutex   = sync.RWMutex{}
	ScheduleCronMap = make(map[int]cron.EntryID)
)

func InitCronService(process *models.Process, errChan chan<- string) *cron.Cron {
	cronProcess := cron.New()
	schedules, err := database.GetSchedules(process.Database)
	if err != nil {
		log.Printf("Cron Service Error with read database, %v", err)
		errChan <- err.Error()
		return nil
	}
	log.Println("Starting init cron schedules")
	for _, schedule := range schedules {
		t, err := time.Parse(time.RFC3339, schedule.TimeMark)
		if err != nil {
			log.Printf("Cron Service Error with parse time, %v", err)
			errChan <- err.Error()
			return nil
		}
		schedule.CronTime, err = ApplyCronTimeFormat(t.Format(layout))
		if err != nil {
			log.Printf("Cron Service Error with apply cron time, %v", err)
			errChan <- err.Error()
			return nil
		}
		cronID, err := cronProcess.AddFunc(schedule.CronTime, CronFunc(schedule, process.Client))
		if err != nil {
			log.Printf("Cron Service Error with add schedule, %v", err)
			errChan <- err.Error()
			return nil
		}
		ScheduleMutex.RLock()
		ScheduleCronMap[schedule.ID] = cronID
		ScheduleMutex.RUnlock()
	}
	log.Println("End init cron schedules")

	log.Println("Cron Service Start")
	cronProcess.Start()

	go func() {
		for {
			select {
			case <-process.Ctx.Done():
				log.Println("Cron Process is Stop")
				return
			}
		}
	}()
	return cronProcess

}

func CronFunc(schedule models.Schedule, client mqtt.Client) func() {
	return func() {
		log.Printf("Running cron job %v\n", schedule.CronTime)
		log.Println(schedule)
		commandPayload := map[string]interface{}{
			schedule.Expose.Property: schedule.CommandData,
		}

		fmt.Println(commandPayload)
		payload, err := json.Marshal(commandPayload)
		if err != nil {
			log.Printf("Cron Service Error with marshal command, %v", err)
			return
		}

		_, ok := database.DevMap[schedule.IEEEName]
		if !ok {
			log.Printf("Cron Service Error with wrong IEEE name, %v\n", schedule.IEEEName)
			return
		}
		token := client.Publish(fmt.Sprintf("zigbee2mqtt/%s/set", schedule.IEEEName), 0, false, payload)
		token.Wait()
		if token.Error() != nil {
			log.Printf("Error publish message: %w", err)
			return
		}

		log.Printf("End cron job %v\n", schedule.CronTime)

	}
}

func ApplyCronTimeFormat(timeMark string) (string, error) {
	t, err := time.Parse(layout, timeMark)
	if err != nil {
		log.Println("Error parsing time_mark:", err)
		return "", fmt.Errorf("error parsing time_mark %v", err)
	}
	cornTime := fmt.Sprintf("%d %d %d %d *", t.Minute(), t.Hour(), t.Day(), int(t.Month()))

	return cornTime, nil
}
