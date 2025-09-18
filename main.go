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

var userSubscriptions = make(map[string][]string)
var courses_links = make(map[string]string)
var running bool = false

func main() {
	godotenv.Load()
	Token := os.Getenv("DISCORD_TOKEN_ID")
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		log.Fatal("Failed to initialize bot")
	}

	loadCourses()
	loadSubscriptions()
	loadLatestAnnouncement()

	dg.AddHandler(commands)
	dg.Identify.Intents = discordgo.IntentsGuildMessages

	if err = dg.Open(); err != nil {
		log.Fatalln()
	}

	log.Println("Bot running")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	dg.Close()
}

func loadCourses() {
	courses_links["OC"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/OC112/2025-2026/1-semestre/rss/announcement"
	courses_links["Aprendizagem"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/Apre2222/2025-2026/1-semestre/rss/announcement"
	courses_links["RC"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/RC112/2025-2026/1-semestre/rss/announcement"
	courses_links["AMS"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/Mod112/2025-2026/1-semestre/rss/announcement"
}

func loadSubscriptions() {
	for k := range courses_links {
		userSubscriptions[k] = make([]string, 0)
	}
}

func loadLatestAnnouncement() {

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

	if strings.Contains(m.Content, "!follow") {
		follow(s, m)
	}

}

func help(s *discordgo.Session, m *discordgo.MessageCreate) {
	helpMsg := "## Commands:\n"
	helpMsg += "- !startfenix - Start service\n"
	helpMsg += "- !follow <Course> - Get notified when new announcements are published in the given course\n"
	helpMsg += "## Courses\n"
	for course := range courses_links {
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

	if _, ok := courses_links[course]; !ok {
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

func fenixFetcher(s *discordgo.Session, m *discordgo.MessageCreate) {
	running = true

	s.ChannelMessageSend(m.ChannelID, "Starting Service")

	for {
		time.Sleep(5 * time.Second)
		data, err := fenixgoscraper.Scrape(courses_links, 1)

		if err != nil {
			log.Fatal(err)
		}

		for course, announcements := range data {
			for _, announcement := range announcements {
				s.ChannelMessageSend(m.ChannelID, parseAnnouncements(announcement, course))
			}
		}
	}
}
