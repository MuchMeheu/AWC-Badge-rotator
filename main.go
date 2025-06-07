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
	effectiveBadgesDir := badgesDir
	if os.Getenv("VERCEL") == "1" {
		log.Println("Running in Vercel environment.")
	}
	err := filepath.WalkDir(effectiveBadgesDir, func(path string, d fs.DirEntry, errWalk error) error {
		if errWalk != nil {
			log.Printf("Error accessing path %q: %v\n", path, errWalk)
			return errWalk
		}
		if !d.IsDir() && (strings.HasSuffix(strings.ToLower(d.Name()), ".gif") || strings.HasSuffix(strings.ToLower(d.Name()), ".png")) {
			discovered = append(discovered, d.Name())
		}
		return nil
	})
	if err != nil {
		log.Printf("Error during badge discovery walk: %v\n", err)
		return
	}
	if len(discovered) > 0 {
		sort.Strings(discovered)
		badgeFilesList = discovered
		log.Printf("Discovered %d badges (GIFs and PNGs): %v\n", len(badgeFilesList), badgeFilesList)
	} else {
		log.Println("No .gif or .png badges found in the directory.")
		badgeFilesList = []string{}
	}
	lastDiscoveryTime = time.Now()
}

func Handler(w http.ResponseWriter, r *http.Request) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", rootHandlerInternal)
	mux.HandleFunc("/badge.gif", badgeHandlerInternal)
	mu.Lock()
	shouldDiscover := len(badgeFilesList) == 0 || time.Since(lastDiscoveryTime) > discoveryInterval
	mu.Unlock()
	if shouldDiscover {
		discoverBadges()
	}
	mux.ServeHTTP(w, r)
}

func rootHandlerInternal(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Go Animated Badge Rotator (Slot-based, Vercel). Use /badge.gif?slot=N")
}

func badgeHandlerInternal(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	if len(badgeFilesList) == 0 {
		mu.Unlock()
		log.Println("[InternalBadge] No badges available (initial check).")
		http.Error(w, "No badges available", http.StatusNotFound)
		return
	}

	localBadgeFilesList := make([]string, len(badgeFilesList))
	copy(localBadgeFilesList, badgeFilesList)
	mu.Unlock()

	if len(localBadgeFilesList) == 0 {
		log.Println("[InternalBadge] Copied badge list is empty (should not happen if initial check passed).")
		http.Error(w, "No badges available after copy", http.StatusNotFound)
		return
	}

	timeWindowSeconds := 2
	baseSeed := time.Now().Unix() / int64(timeWindowSeconds)
	slotStr := r.URL.Query().Get("slot")
	slot, err := strconv.Atoi(slotStr)
	if err != nil || slot < 1 {
		slot = 1
	}

	var selectedFilename string
	tempIndices := make([]int, len(localBadgeFilesList))
	for i := range tempIndices {
		tempIndices[i] = i
	}

	shuffleRand := rand.New(rand.NewSource(baseSeed))
	shuffleRand.Shuffle(len(tempIndices), func(i, j int) { tempIndices[i], tempIndices[j] = tempIndices[j], tempIndices[i] })

	effectiveSlotIndex := (slot - 1)
	if len(tempIndices) > 0 {
		effectiveSlotIndex = effectiveSlotIndex % len(tempIndices)
	} else {
		log.Println("[InternalBadge] Error: tempIndices (shuffled indices) is empty. Cannot select badge.")
		http.Error(w, "Error selecting badge (empty internal list)", http.StatusInternalServerError)
		return
	}

	if effectiveSlotIndex < len(tempIndices) {
		selectedFilename = localBadgeFilesList[tempIndices[effectiveSlotIndex]]
	} else {
		if len(localBadgeFilesList) > 0 {
			selectedFilename = localBadgeFilesList[0]
			log.Printf("Warning: Effective slot index %d out of bounds for tempIndices (len %d), serving first available badge.", effectiveSlotIndex, len(tempIndices))
		} else {
			log.Println("[InternalBadge] Error: No badges in local list for selection after all checks.")
			http.Error(w, "Error selecting badge", http.StatusInternalServerError)
			return
		}
	}

	filePath := filepath.Join(badgesDir, selectedFilename)
	log.Printf("Slot %d (TimeSeed %d): Attempting to serve badge: %s (from path: %s)\n", slot, baseSeed, selectedFilename, filePath)

	if _, statErr := os.Stat(filePath); os.IsNotExist(statErr) {
		log.Printf("!!! File NOT FOUND at path: %s\n", filePath)
		http.Error(w, fmt.Sprintf("Badge file '%s' not found on server.", selectedFilename), http.StatusNotFound)
		return
	}

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

func main() {
	rand.Seed(time.Now().UnixNano())
	discoverBadges()
	http.HandleFunc("/", rootHandlerInternal)
	http.HandleFunc("/badge.gif", badgeHandlerInternal)
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
	log.Printf("Starting Go Slot-based Animated Badge Rotator server LOCALLY on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start local server: %v\n", err)
	}
}
