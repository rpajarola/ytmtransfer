package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// Config holds OAuth2 configurations for both accounts
type Config struct {
	SourceConfig oauth2.Config
	TargetConfig oauth2.Config
}

var (
	enableTransferLikes          bool = true
	enableCreateMonthlyPlaylists bool = true
	dryRun                       bool = false
)

func main() {
	flag.BoolVar(&dryRun, "n", false, "dry-run: show what would be done without making any changes")
	flag.BoolVar(&enableTransferLikes, "l", true, "transfer likes")
	flag.BoolVar(&enableCreateMonthlyPlaylists, "m", true, "create monthly playlists of liked music")
	flag.Parse()

	ctx := context.Background()

	if dryRun {
		log.Print("DRY-RUN mode enabled — no changes will be made")
	}
	log.Print("Reading credentials from credentials.json")
	// Load client credentials
	b, err := os.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	log.Print("Configuring OAuth2")
	// Configure OAuth2 for both accounts
	config, err := google.ConfigFromJSON(b, youtube.YoutubeReadonlyScope, youtube.YoutubeScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}

	log.Print("getting source_token.json")
	// Get tokens for both accounts
	sourceClient := getClient(config, "source_token.json")
	log.Print("getting target_token.json")
	targetClient := getClient(config, "target_token.json")

	// Create YouTube service instances
	log.Print("creating source youtube service")
	sourceService, err := youtube.NewService(ctx, option.WithHTTPClient(sourceClient))
	if err != nil {
		log.Fatalf("Error creating source YouTube service: %v", err)
	}

	log.Print("creating target youtube service")
	targetService, err := youtube.NewService(ctx, option.WithHTTPClient(targetClient))
	if err != nil {
		log.Fatalf("Error creating target YouTube service: %v", err)
	}

	if enableTransferLikes {
		log.Print("Transfering Likes")

		// Transfer likes
		if err := transferLikes(sourceService, targetService); err != nil {
			log.Fatalf("Error transferring likes: %v", err)
		}
	}

	if enableCreateMonthlyPlaylists {
		log.Print("Creating monthly playlists from Liked Music")
		if err := createMonthlyPlaylists(sourceService, targetService); err != nil {
			log.Fatalf("Error creating monthly playlists: %v", err)
		}
	}
}

func getClient(config *oauth2.Config, tokFile string) *http.Client {
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	codeCh := make(chan string)

	// Start local server

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code != "" {
			fmt.Fprintf(w, "Authorization successful! You can close this window.")
			codeCh <- code
		}
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	//	go server.ListenAndServe()
	//	defer server.Shutdown(context.Background())

	config.RedirectURL = server.URL
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Opening browser for authorization: %v\n", authURL)
	openBrowser(authURL)

	// Wait for code
	authCode := <-codeCh

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token: %v", err)
	}
	return tok
}

func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

type stringSet map[string]struct{}

func newStringSet(strings ...string) stringSet {
	set := make(map[string]struct{}, len(strings))
	for _, str := range strings {
		set[str] = struct{}{}
	}
	return set
}

func (s stringSet) Contains(str string) bool {
	_, ok := s[str]
	return ok
}

func transferLikes(source, target *youtube.Service) error {
	likedItems, err := getLikedMusicItems(source)
	if err != nil {
		return err
	}
	transferredItems, err := getLikedMusicItems(target)
	if err != nil {
		return err
	}
	transferredIDs := make([]string, len(transferredItems))
	for i, item := range transferredItems {
		transferredIDs[i] = item.VideoID
	}
	alreadyTransferred := newStringSet(transferredIDs...)

	waitTime := 5 * time.Second
	// Like videos in reverse order so the target ends up with the same order as source
	for i := len(likedItems) - 1; i >= 0; i-- {
		videoId := likedItems[i].VideoID
		if alreadyTransferred.Contains(videoId) {
			log.Printf("skipping %v (already liked)", videoId)
			continue
		}
		if dryRun {
			log.Printf("[dry-run] would like video %d/%d: %s", len(likedItems)-i, len(likedItems), videoId)
			continue
		}
		err := target.Videos.Rate(videoId, "like").Do()
		switch {
		case isQuotaError(err):
			return err
		case isRateLimitedError(err):
			log.Printf("Rate limited, retrying in %vs", waitTime)
			time.Sleep(waitTime * time.Second)
			waitTime += 5 * time.Second
		case isServerError(err):
			log.Printf("Server error, retrying in %vs...", waitTime)
			time.Sleep(waitTime * time.Second)
			waitTime += 5 * time.Second
		case err != nil:
			log.Printf("Error liking video %s: %v", videoId, err)
			continue
		}
		log.Printf("Liked video %d/%d: %s\n", len(likedItems)-i, len(likedItems), videoId)
	}

	return nil
}

func isQuotaError(err error) bool {
	if apiErr, ok := err.(*googleapi.Error); ok {
		for _, e := range apiErr.Errors {
			if e.Reason == "quotaExceeded" {
				return true
			}
		}
	}
	return false
}

func isRateLimitedError(err error) bool {
	if apiErr, ok := err.(*googleapi.Error); ok && apiErr.Code == 429 {
		return true
	}
	return false
}

func isServerError(err error) bool {
	if apiErr, ok := err.(*googleapi.Error); ok && apiErr.Code >= 500 {
		return true
	}
	return false
}

type likedMusicItem struct {
	VideoID     string
	PublishedAt time.Time
}

type monthKey struct {
	Year  int
	Month time.Month
}

// listPlaylists returns a title→id map of all playlists owned by the account.
func listPlaylists(s *youtube.Service) (map[string]string, error) {
	result := make(map[string]string)
	pageToken := ""
	for {
		call := s.Playlists.List([]string{"id", "snippet"}).Mine(true).MaxResults(50)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("error listing playlists: %v", err)
		}
		for _, pl := range resp.Items {
			result[pl.Snippet.Title] = pl.Id
		}
		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return result, nil
}

func getLikedMusicItems(s *youtube.Service) ([]likedMusicItem, error) {
	// https://www.reddit.com/r/Lidarr/comments/1mugfon/sync_youtube_playlists_with_lidarr_using_youtubarr/
	playlistID := "LM"
	var items []likedMusicItem
	pageToken := ""
	for {
		call := s.PlaylistItems.List([]string{"snippet"}).PlaylistId(playlistID).MaxResults(50)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("error fetching Liked Music items: %v", err)
		}
		for _, item := range resp.Items {
			t, err := time.Parse(time.RFC3339, item.Snippet.PublishedAt)
			if err != nil {
				log.Printf("warning: could not parse publishedAt %q for %s: %v",
					item.Snippet.PublishedAt, item.Snippet.ResourceId.VideoId, err)
				continue
			}
			items = append(items, likedMusicItem{
				VideoID:     item.Snippet.ResourceId.VideoId,
				PublishedAt: t,
			})
		}
		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}
	log.Printf("Found %d items in Liked Music playlist", len(items))
	return items, nil
}

func getPlaylistVideoIDs(s *youtube.Service, playlistID string) ([]string, error) {
	var ids []string
	pageToken := ""
	for {
		call := s.PlaylistItems.List([]string{"snippet"}).PlaylistId(playlistID).MaxResults(50)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("error fetching playlist items for %s: %v", playlistID, err)
		}
		for _, item := range resp.Items {
			ids = append(ids, item.Snippet.ResourceId.VideoId)
		}
		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return ids, nil
}

func createMonthlyPlaylists(source, target *youtube.Service) error {
	items, err := getLikedMusicItems(source)
	if err != nil {
		return err
	}

	// Group items by month, preserving first-occurrence order.
	byMonth := make(map[monthKey][]likedMusicItem)
	var orderedMonths []monthKey
	seenMonths := make(map[monthKey]bool)
	for _, item := range items {
		k := monthKey{item.PublishedAt.Year(), item.PublishedAt.Month()}
		if !seenMonths[k] {
			orderedMonths = append(orderedMonths, k)
			seenMonths[k] = true
		}
		byMonth[k] = append(byMonth[k], item)
	}
	log.Printf("Spreading %d items across %d monthly playlists", len(items), len(orderedMonths))

	existingPlaylists, err := listPlaylists(target)
	if err != nil {
		return err
	}

	for _, k := range orderedMonths {
		title := fmt.Sprintf("Liked Music %d-%02d", k.Year, int(k.Month))

		targetPlaylistID, exists := existingPlaylists[title]
		if !exists {
			if dryRun {
				log.Printf("[dry-run] would create playlist %q", title)
				continue
			}
			pl, err := target.Playlists.Insert([]string{"snippet", "status"}, &youtube.Playlist{
				Snippet: &youtube.PlaylistSnippet{Title: title},
				Status:  &youtube.PlaylistStatus{PrivacyStatus: "private"},
			}).Do()
			if err != nil {
				return fmt.Errorf("error creating playlist %q: %v", title, err)
			}
			targetPlaylistID = pl.Id
			log.Printf("Created playlist %q (%s)", title, targetPlaylistID)
		} else {
			log.Printf("Playlist %q already exists (%s)", title, targetPlaylistID)
		}

		existingIDs, err := getPlaylistVideoIDs(target, targetPlaylistID)
		if err != nil {
			return err
		}
		alreadyAdded := newStringSet(existingIDs...)

		for _, item := range byMonth[k] {
			if alreadyAdded.Contains(item.VideoID) {
				log.Printf("skipping %s (already in %q)", item.VideoID, title)
				continue
			}
			if dryRun {
				log.Printf("[dry-run] would add %s to %q", item.VideoID, title)
				continue
			}
			_, err := target.PlaylistItems.Insert([]string{"snippet"}, &youtube.PlaylistItem{
				Snippet: &youtube.PlaylistItemSnippet{
					PlaylistId: targetPlaylistID,
					ResourceId: &youtube.ResourceId{
						Kind:    "youtube#video",
						VideoId: item.VideoID,
					},
				},
			}).Do()
			if err != nil {
				log.Printf("Error adding %s to %q: %v", item.VideoID, title, err)
				continue
			}
			log.Printf("Added %s to %q", item.VideoID, title)
		}
	}
	return nil
}

// Helper to open browser automatically
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	}
	if err != nil {
		fmt.Printf("Please open manually: %v\n", url)
	}
}
