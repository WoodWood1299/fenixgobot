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
	"github.com/joho/godotenv"
)

const COURSE_LINKS_FILENAME = "courses.store"
const SUBSCRIPTIONS_FILENAME = "subscriptions.store"

var userSubscriptions = make(map[string][]string)
var coursesLinks = make(map[string]string)
var running bool = false

func main() {
	godotenv.Load()
	Token := os.Getenv("DISCORD_TOKEN_ID")
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

	loadLatestAnnouncements()

	dg.AddHandler(commands)
	dg.Identify.Intents = discordgo.IntentsGuildMessages

	if err = dg.Open(); err != nil {
		log.Fatalln("FATAL: Error initializing discord session", err)
	}

	log.Println("INFO: Bot running")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	if err = storeCourseLinks(); err != nil {
		log.Fatalln("FATAL: Error storing course links", err)
	}

	if err = storeSubscriptions(); err != nil {
		log.Fatalln("FATAL: Error storing subscriptions", err)
	}
	storeLatestAnnouncements()
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
	log.Println("INFO: Storing Subscriptions")

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

func storeLatestAnnouncements() {

}

func parseFile(fileName string) ([]string, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	return strings.Split(string(data), "\n"), err
}

func endsWithNewlineOffset(splitData []string) int {
	if len(splitData)%2 == 1 {
		return 1
	}

	return 0
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

	for i := 0; i < len(splitData)-endsWithNewlineOffset(splitData); i += 2 {
		coursesLinks[splitData[i]] = splitData[i+1]
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

	for i := 0; i < len(splitData)-endsWithNewlineOffset(splitData); i += 2 {
		course := splitData[i]

		for id := range strings.SplitSeq(splitData[i+1], "|") {
			if id == "" {
				continue
			}

			userSubscriptions[course] = append(userSubscriptions[course], id)
		}
	}

	return nil
}

func loadLatestAnnouncements() {

}

func commands(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!help" {
		help(s, m)
	}

	if m.Content == "!startfenix" {
		startfenix(s, m)
	}

	if strings.Split(m.Content, " ")[0] == "!follow" {
		follow(s, m)
	}
}

func help(s *discordgo.Session, m *discordgo.MessageCreate) {
	helpMsg := "## Commands:\n"
	helpMsg += "- !startfenix - Start service\n"
	helpMsg += "- !follow <Course> - Get notified when new announcements are published in the given course\n"
	helpMsg += "## Courses\n"
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

func follow(s *discordgo.Session, m *discordgo.MessageCreate) {
	cmd := strings.Split(m.Content, " ")
	if len(cmd) != 2 {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Command usage: !follow <course>", m.Author.ID))
		return
	}

	course := cmd[1]

	if !courseExists(course) {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Course does not exist", m.Author.ID))
		return
	}

	if userSubbedToCourse(m.Author.ID, course) {
		s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Already subbed to %s", m.Author.ID, course))
		return
	}

	userSubscriptions[course] = append(userSubscriptions[course], m.Author.ID)
	s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@%s> Subbed to %s", m.Author.ID, course))
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

	msg += fmt.Sprintf("%s %s\n", announcement.Message, announcement.Link)
	return msg
}

func fenixFetcher(s *discordgo.Session, m *discordgo.MessageCreate) error {
	running = true

	s.ChannelMessageSend(m.ChannelID, "Starting Service")

	for {
		time.Sleep(5 * time.Second)
		data, err := fenixgoscraper.Scrape(coursesLinks, 1)

		if err != nil {
			log.Fatalln("FATAL: Fetcher failure", err)
		}

		for course, announcements := range data {
			s.ChannelMessageSend(m.ChannelID, parseAnnouncements(announcements[0], course))
		}
	}
}
