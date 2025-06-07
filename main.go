package main

import (
	"fmt"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	badgesDir         = "./badges"
	defaultPort       = "8080"
	discoveryInterval = 5 * time.Minute
	numBadgeSlots     = 3
)

var (
	badgeFilesList    []string
	mu                sync.Mutex
	lastDiscoveryTime time.Time
)

func discoverBadges() {
	mu.Lock()
	defer mu.Unlock()
	log.Printf("Discovering badges in %s...\n", badgesDir)
	var discovered []string
	err := filepath.WalkDir(badgesDir, func(path string, d fs.DirEntry, errWalk error) error {
		if errWalk != nil {
			return errWalk
		}
		if !d.IsDir() && (strings.HasSuffix(strings.ToLower(d.Name()), ".gif") || strings.HasSuffix(strings.ToLower(d.Name()), ".png")) {
			discovered = append(discovered, d.Name())
		}
		return nil
	})
	if err != nil {
		log.Printf("Error during badge discovery: %v\n", err)
		return
	}
	if len(discovered) > 0 {
		sort.Strings(discovered)
		badgeFilesList = discovered
		log.Printf("Discovered %d badges (GIFs and PNGs): %v\n", len(badgeFilesList), badgeFilesList)
	} else {
		log.Println("No .gif or .png badges found.")
		badgeFilesList = []string{}
	}
	lastDiscoveryTime = time.Now()
}

func selectBadgeForSlot(availableBadges []string, baseSeed int64, slot int) (string, []string) {
	if len(availableBadges) == 0 {
		return "", availableBadges
	}

	r := rand.New(rand.NewSource(baseSeed + int64(slot)))

	idxToPick := r.Intn(len(availableBadges))
	selected := availableBadges[idxToPick]

	remainingBadges := make([]string, 0, len(availableBadges)-1)
	remainingBadges = append(remainingBadges, availableBadges[:idxToPick]...)
	remainingBadges = append(remainingBadges, availableBadges[idxToPick+1:]...)

	return selected, remainingBadges
}

func badgeHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	if time.Since(lastDiscoveryTime) > discoveryInterval {
		mu.Unlock()
		discoverBadges()
		mu.Lock()
	}

	if len(badgeFilesList) == 0 {
		mu.Unlock()
		log.Println("No badges available to serve.")
		http.Error(w, "No badges available", http.StatusNotFound)
		return
	}

	currentAvailableBadges := make([]string, len(badgeFilesList))
	copy(currentAvailableBadges, badgeFilesList)
	mu.Unlock()

	timeWindowSeconds := 2
	baseSeed := time.Now().Unix() / int64(timeWindowSeconds)

	slotStr := r.URL.Query().Get("slot")
	slot, err := strconv.Atoi(slotStr)
	if err != nil || slot < 1 || slot > numBadgeSlots {
		log.Printf("Invalid or missing slot parameter '%s', defaulting to behavior for slot 1\n", slotStr)
		slot = 1
	}

	var selectedFilename string
	tempIndices := make([]int, len(currentAvailableBadges))
	for i := range tempIndices {
		tempIndices[i] = i
	}
	shuffleRand := rand.New(rand.NewSource(baseSeed))
	shuffleRand.Shuffle(len(tempIndices), func(i, j int) {
		tempIndices[i], tempIndices[j] = tempIndices[j], tempIndices[i]
	})

	effectiveSlotIndex := (slot - 1) % len(tempIndices)
	if effectiveSlotIndex < len(tempIndices) {
		selectedFilename = currentAvailableBadges[tempIndices[effectiveSlotIndex]]
	} else {
		if len(currentAvailableBadges) > 0 {
			selectedFilename = currentAvailableBadges[0]
			log.Printf("Warning: Effective slot index out of bounds, serving first available badge.")
		} else {
			log.Println("Error: No badges available after attempting slot selection.")
			http.Error(w, "Error selecting badge", http.StatusInternalServerError)
			return
		}
	}

	filePath := filepath.Join(badgesDir, selectedFilename)
	log.Printf("Slot %d (TimeSeed %d): Serving badge: %s\n", slot, baseSeed, filePath)

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate, public, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	if strings.HasSuffix(strings.ToLower(selectedFilename), ".png") {
		w.Header().Set("Content-Type", "image/png")
	} else {
		w.Header().Set("Content-Type", "image/gif")
	}
	http.ServeFile(w, r, filePath)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Go Animated Badge Rotator (Slot-based). Use /badge.gif?slot=1, /badge.gif?slot=2, etc.")
}

func main() {
	discoverBadges()
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/badge.gif", badgeHandler)
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	log.Printf("Starting Go Slot-based Animated Badge Rotator server on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v\n", err)
	}
}
