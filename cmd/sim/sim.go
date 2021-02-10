package main

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// SelfBase string used for identifying SCEF host in the Self field
const SelfBase string = "http://127.0.0.1:27900/3gpp-monitoring/v1/vzmode_monte/subscriptions/"

// SubscriptionType represents subscribers
// using expiration date or maximum report to subscribe to SCEF
type SubscriptionType byte

// Different enumerations of subscription model used
const (
	ExpirationDate SubscriptionType = 1
	MaximumReport  SubscriptionType = 2
)

// Location is used to specify age of location and cellId
type Location struct {
	Age    uint32 `json:"ageOfLocationInfo"`
	CellID string `json:"cellId"`
}

// LocationReport are used to inform client of UE information
type LocationReport struct {
	MSISDN         string   `json:"msisdn"`
	LocationInfo   Location `json:"locationInfo"`
	MonitoringType string   `json:"monitoringType"`
}

// MonitoringEvent are objects published back to client about the UE
type MonitoringEvent struct {
	Subscription          string           `json:"subscription"`
	MonitoringEventReport []LocationReport `json:"monitoringEventReports"`
}

// Subscriber describes a SCEF subscription
type Subscriber struct {
	Type                    SubscriptionType `json:"-"`
	CellID                  string           `json:"-"`
	Self                    string           `json:"self"`
	MSISDN                  string           `json:"msisdn"`
	SupportedFeatures       string           `json:"supportedFeatures"`
	MonitoringType          string           `json:"monitoringType"`
	LocationType            string           `json:"locationType"`
	Accuracy                string           `json:"accuracy"`
	NotificationDestination string           `json:"notificationDestination"`
	ExpirationTime          time.Time        `json:"monitorExpireTime,string,omitempty"`
	MaximumReport           uint32           `json:"maximumNumberOfReports,string,omitempty"`
}

// UpdateRequest describe inputs for an update request
type UpdateRequest struct {
	CellID string `json:"cellID"`
}

// SubscriptionStore represents a map that store subscribers
type SubscriptionStore struct {
	mapStore map[string]*Subscriber
	lock     sync.RWMutex
}

// NumberOfCellIDs is the number of cells used in simulation
const NumberOfCellIDs = 10

func getCellID(number int) string {
	return []string{
		"311-480-770300",
		"311-480-770301",
		"311-480-770302",
		"311-480-770303",
		"311-480-770304",
		"311-480-770305",
		"311-480-770306",
		"311-480-770307",
		"311-480-770308",
		"311-480-770309",
	}[number]
}

// PostToCAAS handles POST request to CAAS
func PostToCAAS(subscriber Subscriber, wait int) {
	monitoringEvent := MonitoringEvent{
		Subscription: subscriber.Self,
		MonitoringEventReport: []LocationReport{
			{
				MSISDN:         subscriber.MSISDN,
				MonitoringType: subscriber.MonitoringType,
				LocationInfo: Location{
					Age:    0,
					CellID: subscriber.CellID,
				},
			},
		},
	}
	reqbody, err := json.Marshal(monitoringEvent)
	if err != nil {
		log.Println("PostToCAAS: cannot send POST request, something wrong with encoding")
		return
	}
	time.Sleep(time.Duration(wait) * time.Second)
	log.Printf("PostToCAAS: waiting %d seconds to send notification to %s", wait, subscriber.NotificationDestination)
	log.Printf("PostToCAAS: json %s\n", string(reqbody))
	resp, err := http.Post(subscriber.NotificationDestination, "application/json", bytes.NewBuffer(reqbody))
	if err != nil {
		log.Println("PostToCAAS: POST request failed")
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("PostToCAAS: Something bad happen with reading POST body")
	}
	log.Printf("PostToCAAS: POST response: %d, %s\n", resp.StatusCode, (body))
}

// AddSubscriber handles POST requests for subscriptions
func (ss *SubscriptionStore) AddSubscriber(w http.ResponseWriter, req *http.Request) {
	log.Println("AddSubscriber: adding subscriber...")
	mimeType := req.Header.Get("content-type")
	if mimeType != "application/json" {
		log.Printf("SCEFAuthentication: content type not supported...\n")
		http.Error(w, http.StatusText(http.StatusUnsupportedMediaType), http.StatusUnsupportedMediaType)
		return
	}
	// decode subscriber from request body
	subscriber := Subscriber{}
	err := json.NewDecoder(req.Body).Decode(&subscriber)
	if err != nil {
		log.Printf("AddSubscriber: error occured while decode, %s", err)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	// do basic input checking for errors
	if subscriber.NotificationDestination == "" || subscriber.MSISDN == "" ||
		subscriber.MonitoringType != "LOCATION_REPORTING" || subscriber.Accuracy != "CGI_ECGI" {
		log.Printf("AddSubscriber: one of the fields in request body is incorrect, %+v\n", subscriber)
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	if !subscriber.ExpirationTime.IsZero() {
		// expiration time can't be before current time
		if subscriber.ExpirationTime.Before(time.Now()) {
			log.Println("AddSubscriber: expiration time can't be before current time")
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		subscriber.Type = ExpirationDate
	}
	if subscriber.MaximumReport != 0 {
		if subscriber.Type == 0 {
			subscriber.Type = MaximumReport
		} else {
			log.Printf("AddSubscriber: can't specify both reports and expiration field => %s, %d\n",
				subscriber.ExpirationTime, subscriber.MaximumReport)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
	}
	if subscriber.Type == 0 {
		log.Println("AddSubscriber: must specify report or expiration field")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// adding subscriber now
	randCellNumber := rand.Intn(NumberOfCellIDs)
	subID := uuid.New().String()
	subscriber.Self = SelfBase + subID
	subscriber.CellID = getCellID(randCellNumber)
	log.Printf("AddSubscriber: %+v\n", subscriber)
	ss.lock.Lock()
	ss.mapStore[subID] = &subscriber
	ss.lock.Unlock()

	// write back response 201
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(subscriber)

	// setup new struct for response
	wait := rand.Intn(3)
	go PostToCAAS(subscriber, wait)
}

// ListSubscriber handles GET requests for subscriptions
func (ss *SubscriptionStore) ListSubscriber(w http.ResponseWriter, req *http.Request) {
	log.Println("ListSubscriber: listing subscriber...")
	pathVars := mux.Vars(req)
	var subID string
	var found bool

	// do simple error checking
	if subID, found = pathVars["subID"]; !found {
		// if no subscription ID is specified list all of the keys
		keys := []string{}
		ss.lock.RLock()
		for k := range ss.mapStore {
			keys = append(keys, k)
		}
		ss.lock.RUnlock()
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(keys)
		return
	}

	// get a subscriber for the subscription store
	var subscriber *Subscriber
	ss.lock.RLock()
	subscriber, found = ss.mapStore[subID]
	ss.lock.RUnlock()
	if !found {
		log.Printf("ListSubscriber: subscriber ID not found on SCEF...\n")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		return
	}

	// write back response 201
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(subscriber)
}

// RemoveSubscriber handles DELETE requests for subscriptions
func (ss *SubscriptionStore) RemoveSubscriber(w http.ResponseWriter, req *http.Request) {
	log.Println("ListSubscriber: removing subscriber...")
	pathVars := mux.Vars(req)
	var subID string
	var found bool

	// do simple error checking
	if subID, found = pathVars["subID"]; !found {
		log.Printf("ListSubscriber: subscriber ID not found on SCEF...\n")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	// get a subscriber for the subscription store and delete it
	ss.lock.RLock()
	_, found = ss.mapStore[subID]
	ss.lock.RUnlock()
	if found {
		ss.lock.Lock()
		delete(ss.mapStore, subID)
		ss.lock.Unlock()
		w.WriteHeader(http.StatusNoContent)
	} else {
		log.Printf("ListSubscriber: subscriber ID not found on SCEF...\n")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// UpdateSubscriber updates the subscriber
func (ss *SubscriptionStore) UpdateSubscriber(w http.ResponseWriter, req *http.Request) {
	log.Println("UpdateSubscriber: updating subscriber...")
	pathVars := mux.Vars(req)
	var subID string
	var found bool

	// do simple error checking
	if subID, found = pathVars["subID"]; !found {
		log.Printf("UpdateSubscriber: subscriber ID not found on SCEF...\n")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	// get a subscriber for the subscription store and delete it
	ss.lock.RLock()
	_, found = ss.mapStore[subID]
	ss.lock.RUnlock()
	if found {
		// decode subscriber from request body
		updateReq := UpdateRequest{}
		err := json.NewDecoder(req.Body).Decode(&updateReq)
		if err != nil {
			log.Printf("UpdateSubscriber: error occured while decode, %s", err)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		lastChar := string(updateReq.CellID[len(updateReq.CellID)-1])
		idx, err := strconv.ParseInt(lastChar, 10, 32)
		if err != nil || getCellID(int(idx)) != updateReq.CellID {
			log.Printf("UpdateSubscriber: cell id input is not correct, %s", err)
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		ss.lock.Lock()
		ss.mapStore[subID].CellID = getCellID(int(idx))
		ss.lock.Unlock()
		PostToCAAS(*ss.mapStore[subID], 0)
		w.WriteHeader(http.StatusOK)
	} else {
		log.Printf("ListSubscriber: subscriber ID not found on SCEF...\n")
		http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
	}
}

// SCEFStore represents individual SCEF accounts, each with a susbsciprtion mapping
type SCEFStore map[string]*SubscriptionStore

// SCEFHandler is used to find SCEF instance and update their respective subscription stores
func (s SCEFStore) SCEFHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("SCEFAuthentication: looking for SCEF instance...")
	pathVars := mux.Vars(r)
	// check if path format specifies path name
	var SCEFName string
	var found bool
	if SCEFName, found = pathVars["SCEFUser"]; !found {
		log.Printf("SCEFAuthentication: SCEF instance name not found in path...\n")
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	// check if instance exists in the SCEF store
	var SCEFInstance *SubscriptionStore
	if SCEFInstance, found = s[SCEFName]; !found {
		log.Printf("SCEFAuthentication: %s instance not found...\n", SCEFName)
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case "POST":
		SCEFInstance.AddSubscriber(w, r)
	case "GET":
		SCEFInstance.ListSubscriber(w, r)
	case "DELETE":
		SCEFInstance.RemoveSubscriber(w, r)
	case "PUT":
		SCEFInstance.UpdateSubscriber(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ExpirationWatcher is launched every minute to check if any subscriptions expired.
func ExpirationWatcher(SCEF SCEFStore) {
	expirationTicker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-expirationTicker.C:
			for _, scef := range SCEF {
				log.Println("ExpirationWatcher: Cleaning expired entries...")
				scef.lock.RLock()
				for id, subscription := range scef.mapStore {
					scef.lock.RUnlock()
					if time.Now().After(subscription.ExpirationTime) {
						log.Printf("ExpirationWatcher: %s expired\n", id)
						scef.lock.Lock()
						delete(scef.mapStore, id)
						scef.lock.Unlock()
					}
					scef.lock.RLock()
				}
				scef.lock.RUnlock()
			}
		}
	}
}

func main() {
	// create a simple subscription storage
	subStore := &SubscriptionStore{
		mapStore: map[string]*Subscriber{},
		lock:     sync.RWMutex{},
	}
	// TODO: make this configurable and perhaps dynamic in the future
	SCEF := SCEFStore(map[string]*SubscriptionStore{
		"vzmode_monte": subStore,
	})
	go ExpirationWatcher(SCEF)
	// route calls here
	r := mux.NewRouter()
	// add scef user checker
	r.HandleFunc("/{SCEFUser}/subscriptions", SCEF.SCEFHandler).Methods("POST")
	r.HandleFunc("/{SCEFUser}/subscriptions", SCEF.SCEFHandler).Methods("GET")
	r.HandleFunc("/{SCEFUser}/subscriptions/{subID}", SCEF.SCEFHandler).Methods("GET")
	r.HandleFunc("/{SCEFUser}/subscriptions/{subID}", SCEF.SCEFHandler).Methods("DELETE")
	r.HandleFunc("/{SCEFUser}/subscriptions/{subID}", SCEF.SCEFHandler).Methods("PUT")
	log.Fatal(http.ListenAndServe("0.0.0.0:8000", r))
}

//TODO: create handover action using MSISDN '19009001111'
