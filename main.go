package main

import (
	"context"
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
	courseLinksFilename        = "data/courses.data"
	subscriptionsFilename      = "data/subscriptions.data"
	latestAnnouncementFilename = "data/announcements.data"
	storeFolderPath            = "data"
	storeFolderPerms           = 0o755
	storeFilePerms             = 0o666
	fetchInterval              = 5 * time.Second
	backupInterval             = 12 * time.Hour
)

// TODO Make into unordered map
var commands = []*discordgo.ApplicationCommand{
	{
		Name:        "help",
		Description: "Show available commands and courses",
	},
	{
		Name:        "startfenix",
		Description: "Start announcement monitoring service",
	},
	{
		Name:        "subscribe",
		Description: "Subscribe to a course",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "course",
				Description:  "Course name to follow",
				Required:     true,
				Autocomplete: true,
			},
		},
	},
	{
		Name:        "unsubscribe",
		Description: "Unsubscribe from a course",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "course",
				Description:  "Course name to unfollow",
				Required:     true,
				Autocomplete: true,
			},
		},
	},
	{
		Name:        "addcourse",
		Description: "Add a new course to the monitoring system",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "course",
				Description: "Course name",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "rss-link",
				Description: "RSS feed URL for the course. Blue button next to announcements",
				Required:    true,
			},
		},
	},
	{
		Name:        "removecourse",
		Description: "Remove a course from the monitoring system",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:         discordgo.ApplicationCommandOptionString,
				Name:         "course",
				Description:  "Course name",
				Required:     true,
				Autocomplete: true,
			},
		},
	},
}

type Bot struct {
	session             *discordgo.Session
	coursesLinks        map[string]string
	userSubscriptions   map[string][]string
	latestAnnouncements map[string]fenixgoscraper.Announcement
	mu                  sync.RWMutex
	running             bool
	runningMu           sync.Mutex
	fetcherCancel       context.CancelFunc
	backupCancel        context.CancelFunc
	guildID             string
}

func NewBot(token, guildID string) (*Bot, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	bot := &Bot{
		session:             session,
		coursesLinks:        make(map[string]string),
		userSubscriptions:   make(map[string][]string),
		latestAnnouncements: make(map[string]fenixgoscraper.Announcement),
		guildID:             guildID,
	}

	session.AddHandler(bot.handleInteraction)

	session.Identify.Intents = discordgo.IntentsGuilds
	return bot, nil
}

func (b *Bot) Start() error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}

	log.Println("INFO: Registering user slash commands...")

	registeredCommands, err := b.session.ApplicationCommandBulkOverwrite(
		b.session.State.User.ID,
		b.guildID,
		commands,
	)
	if err != nil {
		return fmt.Errorf("failed to register guild commands: %w", err)
	}

	log.Printf("INFO: Registered %d guild commands", len(registeredCommands))

	ctx, cancel := context.WithCancel(context.Background())
	b.backupCancel = cancel

	go func() {
		if err := b.backupHandler(ctx); err != nil && err != context.Canceled {
			log.Printf("WARNING: Backup handler stopped with error: %v\n", err)
		}
	}()

	return nil
}

func (b *Bot) Close() error {
	b.stopFetcher()

	if err := b.session.Close(); err != nil {
		return fmt.Errorf("failed to close discord session: %w", err)
	}

	return nil
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		b.handleCommand(s, i)

	case discordgo.InteractionApplicationCommandAutocomplete:
		b.handleAutocomplete(s, i)
	}
}

func (b *Bot) handleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	switch data.Name {
	case "help":
		b.cmdHelp(s, i)
	case "startfenix":
		b.startfenix(s, i)
	case "subscribe":
		course := data.Options[0].StringValue()
		b.cmdSubscribe(s, i, course)
	case "unsubscribe":
		course := data.Options[0].StringValue()
		b.cmdUnsubscribe(s, i, course)
	case "addcourse":
		course := data.Options[0].StringValue()
		link := data.Options[1].StringValue()
		b.cmdAddCourse(s, i, course, link)
	case "removecourse":
		course := data.Options[0].StringValue()
		b.cmdRemoveCourse(s, i, course)
	}
}

func (b *Bot) handleAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()

	var input string
	for _, opt := range data.Options {
		if opt.Focused {
			input = opt.StringValue()
			break
		}
	}

	choices := b.getMatchingChoices(input)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
	if err != nil {
		log.Printf("WARNING: Failed to respond to Autocomplete: %v", err)
	}
}

func (b *Bot) getMatchingChoices(input string) []*discordgo.ApplicationCommandOptionChoice {
	b.mu.RLock()
	defer b.mu.RUnlock()

	input = strings.ToLower(input)
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)

	for course := range b.coursesLinks {
		if strings.Contains(strings.ToLower(course), input) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  course,
				Value: course,
			})

			if len(choices) >= 25 {
				break
			}
		}
	}

	return choices
}

func (b *Bot) respondToInteraction(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
	if err != nil {
		log.Printf("WARNING: Failed to respond to interaction : %v\n", err)
	}
}

// respondEphemeral sends a private response only visible to the user
func (b *Bot) respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Printf("WARNING: Failed to respond to interaction: %v\n", err)
	}
}

// Command Handlers

func (b *Bot) cmdAddCourse(s *discordgo.Session, i *discordgo.InteractionCreate, course string, link string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// if course exists
	if _, exists := b.coursesLinks[course]; exists {
		b.respondEphemeral(s, i, fmt.Sprintf("Course '%s' already exists", course))
		return
	}

	m := map[string]string{course: link}
	if _, err := fenixgoscraper.Scrape(m, 1); err != nil {
		b.respondEphemeral(s, i, "Invalid link or problem fetching RSS")
		return
	}

	b.coursesLinks[course] = link
	b.userSubscriptions[course] = make([]string, 0)

	b.respondToInteraction(s, i, fmt.Sprintf("Course '%s' added successfully", course))
}

func (b *Bot) cmdRemoveCourse(s *discordgo.Session, i *discordgo.InteractionCreate, course string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// if course doesn't exist
	if _, exists := b.coursesLinks[course]; !exists {
		b.respondEphemeral(s, i, fmt.Sprintf("Course '%s' doesn't exist", course))
	}

	delete(b.coursesLinks, course)
	delete(b.userSubscriptions, course)
	delete(b.latestAnnouncements, course)

	b.respondToInteraction(s, i, fmt.Sprintf("Course '%s' deleted successfully", course))
}

func (b *Bot) cmdHelp(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var helpMsg strings.Builder

	// TODO Make help message follow description from Command array
	helpMsg.WriteString("## Commands:\n")
	helpMsg.WriteString("- /startfenix - Start monitoring\n")
	helpMsg.WriteString("- /subscribe <course> - Get notified when new announcements are published in the given course\n")
	helpMsg.WriteString("- /unsubscribe <course> - Stop getting notifications from the given course\n")
	helpMsg.WriteString("- /addcourse <course> <rss-link> - Add a new course to the monitoring system\n")
	helpMsg.WriteString("- /removecourse <course> - Remove a course from the monitoring system\n")
	helpMsg.WriteString("## Available courses\n")

	b.mu.RLock()
	if len(b.coursesLinks) == 0 {
		helpMsg.WriteString("_No courses available_\n")
	} else {
		for course := range b.coursesLinks {
			fmt.Fprintf(&helpMsg, "- %s\n", course)
		}
	}
	b.mu.RUnlock()
	b.respondToInteraction(s, i, helpMsg.String())
}

func (b *Bot) startfenix(s *discordgo.Session, i *discordgo.InteractionCreate) {
	b.runningMu.Lock()

	if b.running {
		b.runningMu.Unlock()
		b.respondEphemeral(s, i, "Service is already running")
		return
	}

	b.running = true
	b.runningMu.Unlock()

	b.respondToInteraction(s, i, "Starting service")

	ctx, cancel := context.WithCancel(context.Background())
	b.fetcherCancel = cancel

	go func() {
		if err := b.fenixFetcher(ctx, i.ChannelID); err != nil && err != context.Canceled {
			log.Printf("WARNING: Fetcher stopped with error: %v\n", err)
		}
	}()
}

func (b *Bot) cmdSubscribe(s *discordgo.Session, i *discordgo.InteractionCreate, course string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.courseExists(course) {
		b.respondEphemeral(s, i, fmt.Sprintf("Course '%s' does not exist. Use /help to check available courses", course))
		return
	}

	userID := i.Member.User.ID
	if b.userSubbedToCourse(userID, course) {
		b.respondEphemeral(s, i, fmt.Sprintf("Already subbed to '%s'", course))
		return
	}

	b.userSubscriptions[course] = append(b.userSubscriptions[course], userID)
	b.respondToInteraction(s, i, fmt.Sprintf("Following '%s'", course))
}

func (b *Bot) cmdUnsubscribe(s *discordgo.Session, i *discordgo.InteractionCreate, course string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.courseExists(course) {
		b.respondEphemeral(s, i, fmt.Sprintf("Course '%s' does not exist. Use /help to check available courses", course))
		return
	}

	userID := i.Member.User.ID
	if !b.userSubbedToCourse(userID, course) {
		b.respondEphemeral(s, i, fmt.Sprintf("Not subbed to '%s'", course))
		return
	}

	b.userSubscriptions[course] = slices.DeleteFunc(b.userSubscriptions[course], func(s string) bool {
		return s == userID
	})

	b.respondToInteraction(s, i, fmt.Sprintf("Unsubscribed from '%s' successfully", course))
}

// Handler Auxiliary Functions

func (b *Bot) userSubbedToCourse(userID string, key string) bool {
	return slices.Contains(b.userSubscriptions[key], userID)
}

func (b *Bot) courseExists(course string) bool {
	_, ok := b.coursesLinks[course]
	return ok
}

func (b *Bot) sendMessage(s *discordgo.Session, channelID, message string) {
	if _, err := s.ChannelMessageSend(channelID, message); err != nil {
		log.Printf("WARNING: Failed to send message: %v\n", err)
	}
}

func (b *Bot) stopFetcher() {
	b.runningMu.Lock()
	defer b.runningMu.Unlock()

	if b.running && b.fetcherCancel != nil {
		b.fetcherCancel()
		b.running = false
	}
}

func (b *Bot) formatAnnouncements(announcement fenixgoscraper.Announcement, course string) string {
	if announcement.Message == "" {
		return ""
	}

	var msg strings.Builder

	b.mu.RLock()
	subscribers := b.userSubscriptions[course]
	b.mu.RUnlock()

	for _, userID := range subscribers {
		fmt.Fprintf(&msg, "<@%s> ", userID)
	}

	if len(subscribers) > 0 {
		msg.WriteString("\n")
	}

	fmt.Fprintf(&msg, "**%s** (%s)\n", announcement.Message, course)
	fmt.Fprintf(&msg, "[Full Announcement](%s)", announcement.Link)

	return msg.String()
}

// Announcement Fetcher

func (b *Bot) fenixFetcher(ctx context.Context, channelID string) error {
	fetcherTicker := time.NewTicker(fetchInterval)
	defer fetcherTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("INFO: Fenix fetcher stopped")
			return ctx.Err()

		case <-fetcherTicker.C:
			b.mu.RLock()
			courses := make(map[string]string)
			maps.Copy(courses, b.coursesLinks)
			b.mu.RUnlock()

			if len(courses) == 0 {
				continue
			}

			data, err := fenixgoscraper.Scrape(courses, 1)
			if err != nil {
				log.Printf("WARNING: Fetcher error: %v\n", err)
				continue
			}

			for course, announcements := range data {
				if len(announcements) == 0 {
					continue
				}

				latestAnnouncement := announcements[0]
				b.mu.RLock()
				previousAnnouncement, exists := b.latestAnnouncements[course]
				b.mu.RUnlock()

				if exists && previousAnnouncement.Link == latestAnnouncement.Link {
					continue
				}

				b.mu.Lock()
				b.latestAnnouncements[course] = latestAnnouncement
				b.mu.Unlock()

				message := b.formatAnnouncements(latestAnnouncement, course)

				if message != "" {
					b.sendMessage(b.session, channelID, message)
				}
			}
		}
	}
}

func (b *Bot) backupHandler(ctx context.Context) error {
	backupTicker := time.NewTicker(backupInterval)
	defer backupTicker.Stop()

	log.Println("INFO: Starting Backup Handler")

	for {
		select {
		case <-ctx.Done():
			log.Println("INFO: Backup handler stopped")
			return ctx.Err()

		case <-backupTicker.C:
			if err := b.saveAllData(); err != nil {
				log.Printf("WARNING: Error creating backup: %v", err)
			}
		}
	}
}

// Store & Load
func (b *Bot) storeCourseLinks() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var data strings.Builder
	log.Println("INFO: Storing Links")

	for course, link := range b.coursesLinks {
		fmt.Fprintf(&data, "%s\n%s\n", course, link)
	}
	if err := os.WriteFile(courseLinksFilename, []byte(data.String()), storeFilePerms); err != nil {
		return fmt.Errorf("failed to write course links: %w", err)
	}

	return nil
}

func (b *Bot) storeSubscriptions() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var data strings.Builder
	log.Println("INFO: Storing subscriptions")

	for course, userIDs := range b.userSubscriptions {
		fmt.Fprintf(&data, "%s\n", course)
		for _, userID := range userIDs {
			fmt.Fprintf(&data, "%s|", userID)
		}
		data.WriteString("\n")
	}

	if err := os.WriteFile(subscriptionsFilename, []byte(data.String()), storeFilePerms); err != nil {
		return fmt.Errorf("failed to write subscriptions: %w", err)
	}

	return nil
}

func (b *Bot) storeLatestAnnouncements() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var data strings.Builder
	log.Println("INFO: Storing latest announcements")

	for course, announcement := range b.latestAnnouncements {
		fmt.Fprintf(&data, "%s\n%s|%s\n", course, announcement.Message, announcement.Link)
	}

	if err := os.WriteFile(latestAnnouncementFilename, []byte(data.String()), storeFilePerms); err != nil {
		return fmt.Errorf("failed to write announcement: %w", err)
	}

	return nil
}

func parseFile(fileName string) ([]string, error) {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return nil, err
	}

	return strings.Split(string(data), "\n"), nil
}

func (b *Bot) loadCourseLinks() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	log.Println("INFO: Loading courses")

	splitData, err := parseFile(courseLinksFilename)
	if err != nil {
		return err
	}

	for i := 1; i < len(splitData); i += 2 {
		if i >= len(splitData) {
			break
		}
		b.coursesLinks[splitData[i-1]] = splitData[i]
	}

	return nil
}

func (b *Bot) loadSubscriptions() error {
	log.Println("INFO: Loading subscriptions")

	for k := range b.coursesLinks {
		b.userSubscriptions[k] = make([]string, 0)
	}

	splitData, err := parseFile(subscriptionsFilename)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for i := 1; i < len(splitData); i += 2 {
		if i > len(splitData) {
			break
		}

		course := splitData[i-1]
		userIDs := strings.SplitSeq(splitData[i], "|")

		for userID := range userIDs {
			userID = strings.TrimSpace(userID)
			if userID == "" {
				continue
			}

			b.userSubscriptions[course] = append(b.userSubscriptions[course], userID)
		}
	}

	return nil
}

func (b *Bot) loadLatestAnnouncements() error {
	log.Println("INFO: Loading latest announcements")

	splitData, err := parseFile(latestAnnouncementFilename)
	if err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for i := 1; i < len(splitData); i += 2 {
		if i > len(splitData) {
			break
		}

		announcementLine := strings.Split(splitData[i], "|")

		if len(announcementLine) < 2 {
			b.latestAnnouncements[splitData[i-1]] = fenixgoscraper.Announcement{
				Message: "",
				Link:    "",
			}
			continue
		}

		b.latestAnnouncements[splitData[i-1]] = fenixgoscraper.Announcement{
			Message: announcementLine[0],
			Link:    announcementLine[1],
		}
	}

	return nil
}

func checkStoreFolderExists() error {
	if _, err := os.Stat(storeFolderPath); os.IsNotExist(err) {
		log.Printf("INFO: %s folder does not exist, creating...", storeFolderPath)

		if err = os.Mkdir(storeFolderPath, storeFolderPerms); err != nil {
			return err
		}
	}

	return nil
}

func (b *Bot) saveAllData() error {
	if err := checkStoreFolderExists(); err != nil {
		return fmt.Errorf("failed to create store folder: %w", err)
	}

	if err := b.storeCourseLinks(); err != nil {
		return fmt.Errorf("failed to store course links: %w", err)
	}

	if err := b.storeLatestAnnouncements(); err != nil {
		return fmt.Errorf("failed to store latest announcements: %w", err)
	}

	if err := b.storeSubscriptions(); err != nil {
		return fmt.Errorf("failed to store subscriptions: %w", err)
	}

	log.Println("INFO: All data saved successfully")
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("FATAL: %v", &err)
	}
}

func run() error {
	token, foundEnv := os.LookupEnv("DISCORD_TOKEN_ID")

	if !foundEnv {
		log.Fatalln("DISCORD_TOKEN_ID environment variable not set")
	}

	guildID := os.Getenv("DISCORD_GUILD_ID")

	b, err := NewBot(token, guildID)
	if err != nil {
		return err
	}

	defer func() {
		if err := b.Close(); err != nil {
			log.Printf("WARNING: Failed to close bot %v", err)
		}
	}()

	if err = b.loadCourseLinks(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", courseLinksFilename)
	}

	if err = b.loadSubscriptions(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", subscriptionsFilename)
	}

	if err = b.loadLatestAnnouncements(); err != nil {
		log.Printf("WARNING: %s not found. One will be created at the end of execution", latestAnnouncementFilename)
	}

	if err := b.Start(); err != nil {
		return err
	}

	log.Println("INFO: Bot running")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sigChan

	log.Println("INFO: Shutdown signal received, saving data")
	return b.saveAllData()
}
