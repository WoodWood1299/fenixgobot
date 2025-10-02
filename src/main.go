package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/WoodWood1299/fenixgoscraper"
	"github.com/bwmarrin/discordgo"
)

const COURSE_LINKS_FILENAME = ".store/courses.store"
const SUBSCRIPTIONS_FILENAME = ".store/subscriptions.store"
const LATEST_ANNOUNCEMENT_FILENAME = ".store/announcements.store"

var coursesLinks = make(map[string]string)
var userSubscriptions = make(map[string][]string)
var latestAnnouncements = make(map[string]fenixgoscraper.Announcement)
var running bool = false

func main() {
	//godotenv.Load()
	Token, foundEnv := os.LookupEnv("DISCORD_TOKEN_ID")

	if !foundEnv {
		log.Fatalln("FATAL: DISCORD_TOKEN_ID environment variable not set")
	}

	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		log.Fatal("FATAL: Failed to initialize bot, err")
	}

	if err = loadCourseLinks(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", COURSE_LINKS_FILENAME)
	}

	if err = loadSubscriptions(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", SUBSCRIPTIONS_FILENAME)
	}

	if err = loadLatestAnnouncements(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", LATEST_ANNOUNCEMENT_FILENAME)
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

	dg.Close()
}

func storeCourseLinks() error {
	var data string
	log.Println("INFO: Storing Links")

	for course, link := range coursesLinks {
		data += fmt.Sprintf("%s\n%s\n", course, link)
	}
	err := os.WriteFile(COURSE_LINKS_FILENAME, []byte(data), 0666)

	return err
}

func storeSubscriptions() error {
	var data string
	log.Println("INFO: Storing subscriptions")

	for course, userIDs := range userSubscriptions {
		data += fmt.Sprintf("%s\n", course)
		for _, userID := range userIDs {
			data += fmt.Sprintf("%s|", userID)
		}
		data += "\n"
	}

	err := os.WriteFile(SUBSCRIPTIONS_FILENAME, []byte(data), 0666)

	return err
}

func storeLatestAnnouncements() error {
	var data string
	log.Println("INFO: Storing latest announcements")

	for course, announcement := range latestAnnouncements {
		data += fmt.Sprintf("%s\n%s|%s\n", course, announcement.Message, announcement.Link)
	}

	err := os.WriteFile(LATEST_ANNOUNCEMENT_FILENAME, []byte(data), 0666)

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

	splitData, err := parseFile(COURSE_LINKS_FILENAME)
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

	splitData, err := parseFile(SUBSCRIPTIONS_FILENAME)

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
	splitData, err := parseFile(LATEST_ANNOUNCEMENT_FILENAME)

	if err != nil {
		return err
	}

	for i := 1; i < len(splitData); i += 2 {
		announcement_line := strings.Split(splitData[i], "|")
		if len(announcement_line) < 2 {
			latestAnnouncements[splitData[i-1]] = fenixgoscraper.Announcement{Message: "", Link: ""}
			continue
		}
		latestAnnouncements[splitData[i-1]] = fenixgoscraper.Announcement{Message: announcement_line[0], Link: announcement_line[1]}
	}

	return nil
}

func checkStoreFolderExists() error {
	if _, err := os.Stat(".store"); os.IsNotExist(err) {
		log.Println("INFO: .store folder does not exist, creating...")

		err = os.Mkdir(".store", 0755)
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

	case "!help":
		help(s, m)

	case "!startfenix":
		startfenix(s, m)

	case "!follow":
		if len(content) != 2 {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Command usage: !follow <course>", m.Author.ID))
			break
		}

		follow(s, m, content[1])

	case "!unfollow":
		if len(content) != 2 {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Command usage: !follow <course>", m.Author.ID))
			break
		}
		unfollow(s, m, content[1])

	case "!addcourse":
		if len(content) != 3 {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Command usage: !addcourse <course> <link>", m.Author.ID))
			break
		}
		addCourse(s, m, content[1], content[2])
	}
}

func addCourse(s *discordgo.Session, m *discordgo.MessageCreate, course string, link string) {
	if courseExists(course) {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Course already exists", m.Author.ID))
	}

	coursesLinks[course] = link
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> %s added", m.Author.ID, course))
}

func help(s *discordgo.Session, m *discordgo.MessageCreate) {
	helpMsg := "## Commands:\n"
	helpMsg += "- !startfenix - Start service\n"
	helpMsg += "- !follow <course> - Get notified when new announcements are published in the given course\n"
	helpMsg += "- !unfollow <course> - Stop getting notifications from the given course\n"
	helpMsg += "- !addcourse <course> <rss-link> - Add a new course to the notification system\n"
	helpMsg += "## Current courses\n"
	for course := range coursesLinks {
		helpMsg += fmt.Sprintf("- %s\n", course)
	}

	s.ChannelMessageSend(m.ChannelID, helpMsg)
}

func startfenix(s *discordgo.Session, m *discordgo.MessageCreate) {
	if running {
		s.ChannelMessageSend(m.ChannelID, "Already running")
		return
	}

	go fenixFetcher(s, m)
}

func follow(s *discordgo.Session, m *discordgo.MessageCreate, course string) {
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

func unfollow(s *discordgo.Session, m *discordgo.MessageCreate, course string) {
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
	msg := ""
	for i := 0; i < len(userSubscriptions[course]); i++ {
		msg += fmt.Sprintf("<@%s>\n", userSubscriptions[course][i])
	}

	msg += fmt.Sprintf("%s\n", course)
	if announcement.Message == "" {
		return ""
	}

	msg += fmt.Sprintf("%s\n", announcement.Message)
	msg += fmt.Sprintf("%s\n", announcement.Link)
	return msg
}

func fenixFetcher(s *discordgo.Session, m *discordgo.MessageCreate) error {
	running = true

	s.ChannelMessageSend(m.ChannelID, "Starting Service")

	for {
		time.Sleep(5 * time.Second)
		data, err := fenixgoscraper.Scrape(coursesLinks, 1)

		if err != nil {
			log.Println("WARNING: Fetcher failure", err)
		}

		for course, announcements := range data {
			if latestAnnouncements[course].Link == announcements[0].Link {
				continue
			}

			latestAnnouncements[course] = announcements[0]
			s.ChannelMessageSend(m.ChannelID, parseAnnouncements(announcements[0], course))
		}
	}
}
