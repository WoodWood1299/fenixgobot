package main

import (
	"fmt"
	"log"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/WoodWood1299/fenixgoscraper"
	"github.com/bwmarrin/discordgo"
)

const (
	courseLinksFilename        = ".store/courses.store"
	subscriptionsFilename      = ".store/subscriptions.store"
	latestAnnouncementFilename = ".store/announcements.store"
)

var (
	coursesLinks             = make(map[string]string)
	userSubscriptions        = make(map[string][]string)
	latestAnnouncements      = make(map[string]fenixgoscraper.Announcement)
	running             bool = false
	mu                  sync.RWMutex
)

func main() {
	// godotenv.Load()
	Token, foundEnv := os.LookupEnv("DISCORD_TOKEN_ID")

	if !foundEnv {
		log.Fatalln("FATAL: DISCORD_TOKEN_ID environment variable not set")
	}

	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		log.Fatal("FATAL: Failed to initialize bot, err")
	}

	if err = loadCourseLinks(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", courseLinksFilename)
	}

	if err = loadSubscriptions(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", subscriptionsFilename)
	}

	if err = loadLatestAnnouncements(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", latestAnnouncementFilename)
	}

	dg.AddHandler(commands)
	dg.Identify.Intents = discordgo.IntentsGuildMessages

	if err = dg.Open(); err != nil {
		log.Fatalln("FATAL: Error initializing discord session", err)
	}

	log.Println("INFO: Bot running")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	if err = checkStoreFolderExists(); err != nil {
		log.Fatalln("FATAL: Error creating .store folder", err)
	}

	if err = storeCourseLinks(); err != nil {
		log.Fatalln("FATAL: Error storing course links", err)
	}

	if err = storeSubscriptions(); err != nil {
		log.Fatalln("FATAL: Error storing subscriptions", err)
	}

	if err = storeLatestAnnouncements(); err != nil {
		log.Fatalln("FATAL: Error storing latest announcements", err)
	}

	if err = dg.Close(); err != nil {
		log.Fatalln("FATAL: Error closing discordgo ", err)
	}
}

func storeCourseLinks() error {
	var data strings.Builder
	log.Println("INFO: Storing Links")

	for course, link := range coursesLinks {
		fmt.Fprintf(&data, "%s\n%s\n", course, link)
	}
	err := os.WriteFile(courseLinksFilename, []byte(data.String()), 0o666)

	return err
}

func storeSubscriptions() error {
	var data strings.Builder
	log.Println("INFO: Storing subscriptions")

	for course, userIDs := range userSubscriptions {
		fmt.Fprintf(&data, "%s\n", course)
		for _, userID := range userIDs {
			fmt.Fprintf(&data, "%s|", userID)
		}
		data.WriteString("\n")
	}

	err := os.WriteFile(subscriptionsFilename, []byte(data.String()), 0o666)

	return err
}

func storeLatestAnnouncements() error {
	var data strings.Builder
	log.Println("INFO: Storing latest announcements")

	for course, announcement := range latestAnnouncements {
		fmt.Fprintf(&data, "%s\n%s|%s\n", course, announcement.Message, announcement.Link)
	}

	err := os.WriteFile(latestAnnouncementFilename, []byte(data.String()), 0o666)

	return err
}

func parseFile(fileName string) ([]string, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	return strings.Split(string(data), "\n"), err
}

func loadCourseLinks() error {
	/*
		coursesLinks["OC"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/OC112/2025-2026/1-semestre/rss/announcement"
		coursesLinks["Aprendizagem"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/Apre2222/2025-2026/1-semestre/rss/announcement"
		coursesLinks["RC"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/RC112/2025-2026/1-semestre/rss/announcement"
		coursesLinks["AMS"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/Mod112/2025-2026/1-semestre/rss/announcement"
	*/

	log.Println("INFO: Loading courses")

	splitData, err := parseFile(courseLinksFilename)
	if err != nil {
		return err
	}

	for i := 1; i < len(splitData); i += 2 {
		coursesLinks[splitData[i-1]] = splitData[i]
	}

	return nil
}

func loadSubscriptions() error {
	for k := range coursesLinks {
		userSubscriptions[k] = make([]string, 0)
	}

	log.Println("INFO: Loading subscriptions")

	splitData, err := parseFile(subscriptionsFilename)
	if err != nil {
		return err
	}

	for i := 1; i < len(splitData); i += 2 {
		course := splitData[i-1]

		for id := range strings.SplitSeq(splitData[i], "|") {
			if id == "" {
				continue
			}

			userSubscriptions[course] = append(userSubscriptions[course], id)
		}
	}

	return nil
}

func loadLatestAnnouncements() error {
	log.Println("INFO: Storing latest announcements")
	splitData, err := parseFile(latestAnnouncementFilename)
	if err != nil {
		return err
	}

	for i := 1; i < len(splitData); i += 2 {
		announcementLine := strings.Split(splitData[i], "|")
		if len(announcementLine) < 2 {
			latestAnnouncements[splitData[i-1]] = fenixgoscraper.Announcement{Message: "", Link: ""}
			continue
		}
		latestAnnouncements[splitData[i-1]] = fenixgoscraper.Announcement{Message: announcementLine[0], Link: announcementLine[1]}
	}

	return nil
}

func checkStoreFolderExists() error {
	if _, err := os.Stat(".store"); os.IsNotExist(err) {
		log.Println("INFO: .store folder does not exist, creating...")

		err = os.Mkdir(".store", 0o755)
		if err != nil {
			return err
		}
	}

	return nil
}

func commands(s *discordgo.Session, m *discordgo.MessageCreate) {
	content := strings.Split(m.Content, " ")

	switch content[0] {
	case s.State.User.ID:
		return

	case "-help":
		help(s, m)

	case "-startfenix":
		startfenix(s, m)

	case "-follow":
		if len(content) != 2 {
			_, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Command usage: -follow <course>", m.Author.ID))
			if err != nil {
				log.Println("WARNING: Error sending follow reply:", err)
			}

			break
		}

		cmdFollow(s, m, content[1])

	case "-unfollow":
		if len(content) != 2 {
			_, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Command usage: -follow <course>", m.Author.ID))
			if err != nil {
				log.Println("WARNING: Error sending unfollow reply:", err)
			}

			break
		}

		cmdUnfollow(s, m, content[1])

	case "-addcourse":
		if len(content) != 3 {
			_, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Command usage: -addcourse <course> <link>", m.Author.ID))
			if err != nil {
				log.Println("WARNING: Error sending follow reply:", err)
			}

			break
		}

		cmdAddCourse(s, m, content[1], content[2])
	}
}

// COMMANDS

func cmdAddCourse(s *discordgo.Session, m *discordgo.MessageCreate, course string, link string) {
	mu.Lock()
	defer mu.Unlock()

	if courseExists(course) {
		_, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Course already exists", m.Author.ID))
		if err != nil {
			log.Printf("WARNING: Error sending message: %v\n", err)
		}
	}

	coursesLinks[course] = link
	_, err := s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> %s added", m.Author.ID, course))
	if err != nil {
		log.Printf("WARNING: Error sending message: %v\n", err)
	}
}

func help(s *discordgo.Session, m *discordgo.MessageCreate) {
	var helpMsg strings.Builder
	helpMsg.WriteString("## Commands:\n")
	helpMsg.WriteString("- -startfenix - Start service\n")
	helpMsg.WriteString("- -follow <course> - Get notified when new announcements are published in the given course\n")
	helpMsg.WriteString("- -unfollow <course> - Stop getting notifications from the given course\n")
	helpMsg.WriteString("- -addcourse <course> <rss-link> - Add a new course to the notification system\n")
	helpMsg.WriteString("## Current courses\n")
	for course := range coursesLinks {
		fmt.Fprintf(&helpMsg, "- %s\n", course)
	}

	_, err := s.ChannelMessageSend(m.ChannelID, helpMsg.String())
	if err != nil {
		log.Printf("WARNING: Error sending message %v\n", err)
	}
}

func startfenix(s *discordgo.Session, m *discordgo.MessageCreate) {
	if running {
		s.ChannelMessageSend(m.ChannelID, "Already running")
		return
	}

	go fenixFetcher(s, m)
}

func cmdFollow(s *discordgo.Session, m *discordgo.MessageCreate, course string) {
	mu.Lock()
	defer mu.Unlock()

	if !courseExists(course) {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Course does not exist", m.Author.ID))
		return
	}

	if userSubbedToCourse(m.Author.ID, course) {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Already following %s", m.Author.ID, course))
		return
	}

	userSubscriptions[course] = append(userSubscriptions[course], m.Author.ID)
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Followed %s", m.Author.ID, course))
}

func cmdUnfollow(s *discordgo.Session, m *discordgo.MessageCreate, course string) {
	mu.Lock()
	defer mu.Unlock()

	if !courseExists(course) {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Course does not exist", m.Author.ID))
		return
	}

	if !userSubbedToCourse(m.Author.ID, course) {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Not following %s", m.Author.ID, course))
		return
	}

	userSubscriptions[course] = slices.DeleteFunc(userSubscriptions[course], func(s string) bool {
		return s == m.Author.ID
	})
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Unfollowed %s", m.Author.ID, course))
}

func userSubbedToCourse(userID string, key string) bool {
	return slices.Contains(userSubscriptions[key], userID)
}

func courseExists(course string) bool {
	_, ok := coursesLinks[course]
	return ok
}

func parseAnnouncements(announcement fenixgoscraper.Announcement, course string) string {
	var msg strings.Builder
	for i := 0; i < len(userSubscriptions[course]); i++ {
		fmt.Fprintf(&msg, "<@%s>\n", userSubscriptions[course][i])
	}

	fmt.Fprintf(&msg, "%s\n", course)
	if announcement.Message == "" {
		return ""
	}

	fmt.Fprintf(&msg, "%s\n", announcement.Message)
	fmt.Fprintf(&msg, "%s\n", announcement.Link)
	return msg.String()
}

func fenixFetcher(s *discordgo.Session, m *discordgo.MessageCreate) error {
	running = true
	s.ChannelMessageSend(m.ChannelID, "Starting Service")

	for {
		time.Sleep(5 * time.Second)

		mu.RLock()
		coursesCpy := make(map[string]string) // Create a copy to facilitate concurrency
		maps.Copy(coursesCpy, coursesLinks)
		mu.RUnlock()

		data, err := fenixgoscraper.Scrape(coursesCpy, 1)
		if err != nil {
			log.Println("WARNING: Fetcher failure", err)
		}

		for course, announcements := range data {
			latestAnnouncement := announcements[0]

			mu.RLock()
			oldLatestAnnouncement, exists := latestAnnouncements[course]
			mu.RUnlock()

			if exists && oldLatestAnnouncement.Link == latestAnnouncement.Link {
				continue
			}

			mu.Lock()
			latestAnnouncements[course] = announcements[0]
			mu.Unlock()

			_, err := s.ChannelMessageSend(m.ChannelID, parseAnnouncements(latestAnnouncement, course))
			if err != nil {
				log.Printf("WARNING: Error sending message: %v\n", err)
			}
		}
	}
}
