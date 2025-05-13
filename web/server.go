package web

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"

	"SmartGreenHouse/database"
	"SmartGreenHouse/models"
	"SmartGreenHouse/services"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/robfig/cron/v3"
)

func RunWebServer(errChan chan string, process *models.Process, cronProcess *cron.Cron) {

	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/devices", devicesHandler)
	http.HandleFunc("/devices/{deviceName}", devicesNameHandler)
	http.HandleFunc("/devices/{deviceName}/{deviceAction}", devicesActionHandler(process.Client))
	http.HandleFunc("/devices/{deviceName}/chart/{action}", chartActionHandler(process))
	http.HandleFunc("/schedule", scheduleHandler(process, cronProcess))
	http.HandleFunc("/schedule-list", scheduleListHandler(process))
	http.HandleFunc("/schedule/delete", scheduleDeleteHandler(process, cronProcess))
	http.HandleFunc("/scenario", scenarioHandler(process))
	http.HandleFunc("/scenario-create", scenarioCreateHandler(process))
	http.HandleFunc("/scenario-form", scenarioFormHandler(process))
	http.HandleFunc("/scenario-list", scenarioListHandler(process))
	http.HandleFunc("/scenario/device", scenarioDeviceHandler(process))
	http.HandleFunc("/scenario/device-target", scenarioDeviceTargetHandler(process))
	http.HandleFunc("/scenario/delete", scenarioDeleteHandler(process))
	http.HandleFunc("/permit-join", permitJoinHandler(process.Client))

	go func() {
		log.Println("Web server started at http://localhost:8080")
		if err := http.ListenAndServe(":8080", nil); err != nil {
			errChan <- fmt.Sprintf("listen and serv err :%w", err)
		}
	}()

	go func() {
		select {
		case <-process.Ctx.Done():
			log.Println("Web server shutting down")
			return
		}
	}()

}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.ParseFiles("web/templates/index.html"))
	tmpl.Execute(w, nil)
}

func devicesHandler(w http.ResponseWriter, r *http.Request) {
	database.DevicesMu.RLock()
	defer database.DevicesMu.RUnlock()

	tmpl := template.Must(template.ParseFiles("web/templates/devices.html"))
	tmpl.Execute(w, database.Devices)
}

func devicesNameHandler(w http.ResponseWriter, r *http.Request) {
	deviceName := r.PathValue("deviceName")
	log.Printf("devicesNameHandler, deviceName: %s", deviceName)

	database.DevicesMu.RLock()
	dev := database.DevMap[deviceName]
	defer database.DevicesMu.RUnlock()

	tmpl := template.Must(template.ParseFiles("web/templates/extend_device.html"))
	tmpl.Execute(w, dev)
}

func devicesActionHandler(client mqtt.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			log.Println("Error parsing form:", err)
			return
		}

		deviceName := r.PathValue("deviceName")
		deviceActionName := r.PathValue("deviceAction")
		formValue := r.FormValue("value")

		if _, ok := database.DevMap[deviceName]; !ok {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}

		log.Printf("devicesActionHandler, deviceName: %s, dev action: %s, form val %s", deviceName, deviceActionName, formValue)
		actionPayload := map[string]interface{}{
			deviceActionName: formValue,
		}
		payload, err := json.Marshal(actionPayload)
		if err != nil {
			log.Println("Error marshalling payload:", err)
			return
		}
		token := client.Publish(fmt.Sprintf("zigbee2mqtt/%s/set", deviceName), 0, false, payload)
		token.Wait()
		if token.Error() != nil {
			log.Println("Error publishing token:", err)
			return
		}
		log.Println("Set data to device:", deviceName)
		w.Header().Set("HX-Redirect", fmt.Sprintf("/devices/%s", deviceName))
	}
}

func chartActionHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceName := r.PathValue("deviceName")
		actionName := r.PathValue("action")
		device := database.DevMap[deviceName]

		data, err := database.GetDataForChartByAction(device, actionName, process.Database)
		if err != nil {
			log.Println("Error getting data for chart:", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Unsupported type of data for chart"))
			return
		}
		//fmt.Println(data)

		if err != nil {
			log.Println("Error marshalling data:", err)
		}
		tmpl := template.Must(template.ParseFiles("web/templates/chart.html"))
		tmpl.Execute(w, data)

	}
}

func scheduleListHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		schedules, err := database.GetSchedules(process.Database)
		if err != nil {
			log.Println("Error getting schedules:", err)
		}
		tmpl := template.Must(template.ParseFiles("web/templates/schedule_list.html"))
		tmpl.Execute(w, schedules)
	}
}

func scheduleHandler(process *models.Process, cronProcess *cron.Cron) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			log.Println("Error parsing form:", err)
			return
		}
		formIEEEName := r.FormValue("device_ieee_name")
		formCommand := r.FormValue("command")
		formCommandData := r.FormValue("command_data")
		formScheduleTime := r.FormValue("schedule_time")

		if formScheduleTime == "" || formCommand == "" || formIEEEName == "" || formCommandData == "" {
			tmpl := template.Must(template.ParseFiles("web/templates/schedule.html"))
			tmpl.Execute(w, database.Devices)
			return
		}

		_, ok := database.DevMap[formIEEEName]
		if !ok {
			http.Error(w, "Device not found", http.StatusNotFound)
			return
		}

		cornTime, err := services.ApplyCronTimeFormat(formScheduleTime)
		if err != nil {
			log.Println("Error applying cron time format:", err)
			return
		}
		schedule := models.NewSchedule(formIEEEName, formCommand, formCommandData, formScheduleTime, cornTime)
		id, err := cronProcess.AddFunc(schedule.CronTime, services.CronFunc(*schedule, process.Client))
		if err != nil {
			log.Println("Error adding schedule:", err)
			return
		}

		scheduleID, err := database.SaveScheduleData(schedule, process.Database)
		if err != nil {
			log.Println("Error saving schedule:", err)
			return
		}

		services.ScheduleMutex.RLock()
		services.ScheduleCronMap[scheduleID] = id
		services.ScheduleMutex.RUnlock()

		//fmt.Println(formIEEEName, formCommand, formCommandData, formScheduleTime, cornTime)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Zigbee schedule for " + formIEEEName + " created"))
	}
}

func scheduleDeleteHandler(process *models.Process, cronProcess *cron.Cron) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			log.Println("Error parsing form:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		id := r.FormValue("id")
		intID, err := strconv.Atoi(id)
		if err != nil {
			log.Println("Error converting id to int:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		value, ok := services.ScheduleCronMap[intID]
		if !ok {
			log.Println("Error finding scheduled cron:", intID)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		log.Println("Deleting scheduled cron:", intID)
		cronProcess.Remove(value)

		err = database.DeleteSchedule(id, process.Database)
		if err != nil {
			log.Println("Error deleting schedule:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		log.Println("Deleted scheduled cron:", intID)

		w.WriteHeader(http.StatusOK)
	}
}

func scenarioCreateHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("Scenario create handler")

		err := r.ParseForm()
		if err != nil {
			log.Println("Error parsing form:", err)
			return
		}
		IEEEName := r.FormValue("device_ieeename")
		exposeName := r.FormValue("property")
		operator := r.FormValue("operator")
		valueCheck := r.FormValue("value-check")
		IEEENameAction := r.FormValue("action_device_ieeenmae")
		exposeAction := r.FormValue("property-action")
		valueSet := r.FormValue("value-set")

		log.Println(IEEEName, exposeName, operator, valueCheck)
		log.Println(IEEENameAction, exposeAction, valueSet)

		zigbeePubTopic := fmt.Sprintf("zigbee2mqtt/%s/set", IEEENameAction)
		jsonRaw := map[string]interface{}{
			exposeAction: valueSet,
		}

		targetScenario := models.NewScenario(IEEEName, exposeName, operator, valueCheck, zigbeePubTopic, jsonRaw)
		log.Println(targetScenario)
		_, err = database.SaveScenario(*targetScenario, process.Database)
		if err != nil {
			log.Println("Error saving scenario:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		//database.ScenariosMu.RLock()
		//database.ScenarioMap[scenariosID] = *targetScenario
		//database.ScenariosMu.RUnlock()

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("Scenario created"))
	}

}

func scenarioHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles("web/templates/scenario.html"))
		tmpl.Execute(w, nil)
	}
}

func scenarioFormHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		tmpl := template.Must(template.ParseFiles("web/templates/scenario_form.html"))
		tmpl.Execute(w, database.Devices)
	}
}

func scenarioListHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		tmpl := template.Must(template.ParseFiles("web/templates/scenario_list.html"))
		tmpl.Execute(w, database.Scenarios)
	}
}

func scenarioDeviceHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceIEEEName := r.FormValue("device_ieeename")
		log.Println("Scenario device:", deviceIEEEName)
		device := database.DevMap[deviceIEEEName]
		tmpl := template.Must(template.ParseFiles("web/templates/scenario_device.html"))
		tmpl.Execute(w, device)
	}
}

func scenarioDeviceTargetHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deviceIEEEName := r.FormValue("action_device_ieeenmae")
		log.Println("Scenario device:", deviceIEEEName)
		device := database.DevMap[deviceIEEEName]
		tmpl := template.Must(template.ParseFiles("web/templates/scenario_device_target.html"))
		tmpl.Execute(w, device)
	}
}

func scenarioDeleteHandler(process *models.Process) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			log.Println("Error parsing form:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		id := r.FormValue("id")
		intID, err := strconv.Atoi(id)
		log.Println("Deleting scenario id :", intID)
		err = database.DeleteScenario(id, process.Database)
		if err != nil {
			log.Println("Error deleting scheduled cron:", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}

}

func permitJoinHandler(client mqtt.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		permitJoinPayload := map[string]interface{}{
			"time": 50,
		}
		payload, err := json.Marshal(permitJoinPayload)
		if err != nil {
			http.Error(w, "Failed to encode payload", http.StatusInternalServerError)
			return
		}

		token := client.Publish("zigbee2mqtt/bridge/request/permit_join", 0, false, payload)
		token.Wait()
		if token.Error() != nil {
			http.Error(w, "Failed to send MQTT message", http.StatusInternalServerError)
			return
		}

		log.Println("Permit join enabled")
	}
}
