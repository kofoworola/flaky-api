package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	endpoint = "http://app-homevision-staging.herokuapp.com/api_project/houses?page=%d"
	maxTries = 20
)

var (
	client http.Client
	wg     sync.WaitGroup
)

type houses struct {
	Houses []house `json:"houses"`
}

type house struct {
	ID        int    `json:"id"`
	Address   string `json:"address"`
	Homeowner string `json:"homeowner"`
	Price     int    `json:"price"`
	PhotoURL  string `json:"photoURL"`
}

func main() {
	// use a client of timeout of 30 seconds for all requests to avoid hang
	client = http.Client{
		Timeout: time.Second * 30,
	}

	// don't need to use url.URL for now since it's a const endpoint we are pinging
	allHouses := make([]house, 0)
	for i := 1; i <= 10; i++ {
		pageEndpoint := fmt.Sprintf(endpoint, i)
		resp, err := requestWithRetry(pageEndpoint)
		if err != nil {
			log.Fatalf("error requesting %s: %v", pageEndpoint, err)
		}
		defer resp.Body.Close()

		// decode the response and append to the array of all houses
		var houses houses
		if err := json.NewDecoder(resp.Body).Decode(&houses); err != nil {
			log.Fatalf("error decoding json response: %v", err)
		}
		allHouses = append(allHouses, houses.Houses...)
	}
	fmt.Printf("there is a total of %d houses", len(allHouses))

	start := time.Now()
	// begin downloads
	for _, house := range allHouses {
		go downloadHouseImage(house)
		wg.Add(1)
	}

	wg.Wait()
	fmt.Printf("\nit took %s to complete\n", time.Now().Sub(start).String())

}

func requestWithRetry(url string) (*http.Response, error) {
	var (
		resp       *http.Response
		successful bool
		tries      int
		err        error
	)
	for !successful {
		// if the amount of tries is more than the max, error out
		if tries >= maxTries {
			log.Fatalf("endpoint %s was consistently failing", url)
		}
		resp, err = client.Get(url)
		if err != nil {
			break
		}
		if resp.StatusCode == http.StatusOK {
			successful = true
			break
		}
		tries++
	}
	return resp, err
}

// downloadHouseImage downloads the image attached to the file
// it downloads it by copying directly from the response body reader,
// to avoid reading the file contents to memory first
func downloadHouseImage(house house) {
	defer wg.Done()
	fileName := fmt.Sprintf(
		"id-%d-%s.%s",
		house.ID,
		strings.TrimRight(house.Address, "."),
		filepath.Ext(house.PhotoURL),
	)

	logger := log.New(os.Stdout, "\n"+fileName, log.Ltime)
	resp, err := client.Get(house.PhotoURL)
	if err != nil {
		logger.Printf("error getting photo: %v", err)
		return
	}
	defer resp.Body.Close()

	// create the file
	file, err := os.Create(fileName)
	if err != nil {
		logger.Printf("could not create file: %v", err)
		return
	}
	defer file.Close()

	// copy the contents
	if _, err := io.Copy(file, resp.Body); err != nil {
		logger.Printf("error copying file contents: %v", err)
		return
	}

}
