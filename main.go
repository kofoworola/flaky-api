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
	endpoint      = "http://app-homevision-staging.herokuapp.com/api_project/houses?page=%d"
	maxTries      = 20
	maxGoroutines = 20
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

// how this works is we use a pipeline so that both the fetch and downloads can be performed concurrently
// this increased runtime by an estimate ~12s
func main() {
	// use a client of timeout of 30 seconds for all requests to avoid hang
	client = http.Client{
		Timeout: time.Second * 30,
	}

	housesPipeline := pipeHouses()

	// dispatch a set of goroutines to listen to and download images.
	// maxGoroutines can be tweaked for scalability and downloading in the thousands.
	// for now, a lower number is fine to avoid delay from creating and releasing the goroutines
	for i := 0; i < maxGoroutines; i++ {
		wg.Add(1)
		go downloadHouseImages(housesPipeline)
	}
	wg.Wait()
	//	fmt.Printf("\nAll Image download took %s to complete\n", time.Now().Sub(start).String())
}

// pipeHouse returns a channel of houses, and dispatches a goroutine that loads each page
// then send the responses to the channel
func pipeHouses() <-chan house {
	out := make(chan house)
	go func() {
		for i := 1; i <= 10; i++ {
			pageEndpoint := fmt.Sprintf(endpoint, i)
			resp, err := requestWithRetry(pageEndpoint)
			if err != nil {
				log.Fatalf("error requesting %s: %v", pageEndpoint, err)
			}
			defer resp.Body.Close()

			var houses houses
			if err := json.NewDecoder(resp.Body).Decode(&houses); err != nil {
				log.Fatalf("error decoding json response: %v", err)
			}

			for _, house := range houses.Houses {
				out <- house
			}
		}
		close(out)

	}()

	return out
}

// downloadHouseImages downloads the image attached to the file
// it downloads it by copying directly from the response body reader,
// to avoid reading the file contents to memory first.
// It downloads by using an infinite loop to listen in on the input channel and get to work
// once the input channel is closed, the loop is broken out of and the wait group is released
func downloadHouseImages(in <-chan house) {
	for {
		if house, ok := <-in; ok {
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
		} else {
			break
		}
	}
	wg.Done()
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
