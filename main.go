package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/WoodWood1299/fenixgoscraper"
	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	Token := os.Getenv("DISCORD_TOKEN_ID")
	dg, err := discordgo.New("Bot " + Token)
	if err != nil {
		log.Fatal("Failed to initialize bot")
	}

	dg.AddHandlerOnce(messageCreate)
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

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	if m.Content == "!startfenix" {
		go fenixUpdater(s, m)
	}
}

func fenixUpdater(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, "RAHHHHHHHH")

	disciplina_links := make(map[string]string)
	disciplina_links["OC"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/OC112/2025-2026/1-semestre/rss/announcement"
	disciplina_links["Aprendizagem"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/Apre2222/2025-2026/1-semestre/rss/announcement"
	disciplina_links["RC"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/RC112/2025-2026/1-semestre/rss/announcement"
	disciplina_links["AMS"] = "https://fenix.tecnico.ulisboa.pt/disciplinas/Mod112/2025-2026/1-semestre/rss/announcement"

	ans, err := fenixgoscraper.Scrape(disciplina_links, 1)

	if err != nil {
		log.Fatal(err)
	}

	for {
		time.Sleep(5 * time.Second)
		for disciplina, announcements := range ans {
			msg := ""
			//s.ChannelMessageSend(m.ChannelID,i disciplina)
			msg += fmt.Sprintf("%s\n", disciplina)
			for _, announcement := range announcements {
				if announcement.Message == "" {
					continue
				}
				msg += fmt.Sprintf("%s\n", fenixgoscraper.StringAnnouncement(announcement))
			}
			s.ChannelMessageSend(m.ChannelID, msg)
		}
	}
}
