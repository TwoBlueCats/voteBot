package main

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/multierr"
	tele "gopkg.in/telebot.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	glog "gorm.io/gorm/logger"
)

func sendToAdmins(b *tele.Bot, admins []int64, what interface{}, opts ...interface{}) ([]*tele.Message, error) {
	var mErr error
	var messages []*tele.Message
	for _, id := range admins {
		msg, err := b.Send(tele.ChatID(id), what)
		messages = append(messages, msg)
		if err != nil {
			mErr = multierr.Append(mErr, err)
		}
	}
	return messages, mErr
}

const (
	pollPrefixCmd = "/poll_"
	voteUnique    = "vote_callback"
)

func prepareInlineVotes(poll Poll) *tele.ReplyMarkup {
	lines := make([]tele.Row, 0)
	selectors := &tele.ReplyMarkup{}
	for idx, variant := range poll.Variants {
		lines = append(lines, selectors.Row(selectors.Data(variant, voteUnique, poll.ID.String(), strconv.Itoa(idx))))
	}

	selectors.Inline(lines...)
	return selectors
}

func pollInfo(c tele.Context, db *gorm.DB) error {
	pollPrefix := strings.TrimPrefix(c.Text(), pollPrefixCmd)
	authorID := c.Message().Sender.ID
	var process Poll
	txGet := db.Where("author_id = ? AND id::text LIKE ?", authorID, pollPrefix+"%").Take(&process)
	if txGet.Error != nil && !errors.Is(txGet.Error, gorm.ErrRecordNotFound) {
		fmt.Println(">>", txGet.Error)
		return c.Send("Got error, try again")
	}
	if txGet.RowsAffected == 0 {
		return c.Send("You have no new poll, send /create to start new poll")
	}

	message := "Poll " + strings.ReplaceAll(pollPrefixCmd, "_", "\\_") + process.ID.String()[:8] + "\n\n"
	message += pollVotesToText(process, db)

	return c.Send(message, &tele.SendOptions{ParseMode: tele.ModeMarkdownV2, ReplyMarkup: prepareInlineVotes(process)})
}

func main() {
	rand.Seed(time.Now().Unix())

	dbDSN := os.Getenv("DBDSN")
	fmt.Println(dbDSN)
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dbDSN,
		PreferSimpleProtocol: true,
	}), &gorm.Config{Logger: glog.Default.LogMode(glog.Info)})

	if err != nil {
		fmt.Println(err)
		panic("failed to connect database")
	}

	pref := tele.Settings{
		Token:   os.Getenv("BOT_TOKEN"),
		Poller:  &tele.LongPoller{Timeout: 10 * time.Second},
		Verbose: os.Getenv("DEBUG") != "",
	}

	adminsEnv := os.Getenv("BOT_ADMINS")
	if len(adminsEnv) == 0 {
		log.Fatal("no bot admins")
		return
	}

	admins := make([]int64, 0)
	for _, id := range strings.Split(adminsEnv, ",") {
		value, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			log.Fatal(err)
			return
		}
		admins = append(admins, value)
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		log.Fatal(err)
		return
	}
	log.Print("Init: ok")

	_, err = sendToAdmins(b, admins, "I started")
	if err != nil {
		log.Fatal(err)
		return
	}

	b.Handle("/hello", func(c tele.Context) error {
		return c.Send("Hello, " + c.Message().Sender.Username + "!")
	})

	b.Handle("/me", func(c tele.Context) error {
		sender := c.Message().Sender
		message := "Hello, @" + sender.Username + "\\!\n"
		message += "Your id is `" + c.Message().Sender.Recipient() + "`\n"
		return c.Send(message, &tele.SendOptions{ParseMode: tele.ModeMarkdownV2})
	})

	b.Handle("/report", func(c tele.Context) error {
		_, err := sendToAdmins(b, admins, "New report:\n"+c.Message().Payload)
		if err != nil {
			return c.Send("Please try again")
		}
		return c.Send("Thank for your report")
	})

	b.Handle("/create", func(c tele.Context) error {
		authorID := c.Message().Sender.ID

		var process Poll
		txGet := db.Where("author_id = ? AND state NOT IN ?", authorID, []string{string(statusOpen), string(statusClose)}).Take(&process)
		if txGet.Error != nil && !errors.Is(txGet.Error, gorm.ErrRecordNotFound) {
			fmt.Println(">>", txGet.Error)
			return c.Send("Got error, try again")
		}
		if txGet.RowsAffected != 0 {
			return c.Send("You already have new poll, please finish it before creating new.")
		}

		tx := db.Create(&Poll{AuthorID: authorID})
		if tx.Error != nil {
			fmt.Println(">>", txGet.Error)
			return c.Send("Got error, try again")
		}
		return c.Send("Good. Now send me the question.")
	})

	b.Handle("/polls", func(c tele.Context) error {
		authorID := c.Message().Sender.ID
		var polls []Poll

		if tx := db.Where(&Poll{AuthorID: authorID}).Order("update_dt desc").Limit(selectLimit).Find(&polls); tx.Error != nil {
			fmt.Println(">>", tx.Error)
			return c.Send("Got error, try again")
		}

		if len(polls) == 0 {
			return c.Send("You have no polls.\nSend /create to start new poll")
		}

		message := "Your last polls:\n"
		for _, poll := range polls {
			message += "• /poll_" + poll.ID.String()[:8] + " " + poll.Question.String + "\n"
		}
		return c.Send(message)
	})

	b.Handle("/done", func(c tele.Context) error {
		authorID := c.Message().Sender.ID
		var process Poll
		txGet := db.Where("author_id = ? AND state NOT IN ?", authorID, []string{string(statusOpen), string(statusClose)}).Take(&process)
		if txGet.Error != nil && !errors.Is(txGet.Error, gorm.ErrRecordNotFound) {
			fmt.Println(">>", txGet.Error)
			return c.Send("Got error, try again")
		}
		if txGet.RowsAffected == 0 {
			return c.Send("You have no new poll, send /create to start new poll")
		}

		if process.State == statusText && len(process.Variants) >= 2 {
			process.State = statusOpen
			db.Save(&process)
			return c.Send("Your poll is created!")
		} else if process.State == statusText {
			return c.Send("You need two or more variants to finish poll")
		} else {
			return c.Send("You have no new poll, send /create to start new poll")
		}
	})

	b.Handle(tele.OnQuery, func(c tele.Context) error {
		authorID := c.Query().Sender.ID
		var polls []Poll

		if tx := db.Where(&Poll{AuthorID: authorID, State: statusOpen}).Order("update_dt desc").Limit(selectLimit).Find(&polls); tx.Error != nil {
			fmt.Println(">>", tx.Error)
			return c.Send("Got error, try again")
		}

		if len(polls) == 0 {
			return c.Send("You have no polls.")
		}

		results := make(tele.Results, 0)
		for idx, poll := range polls {
			results = append(results, &tele.ArticleResult{
				Title:       poll.Question.String,
				Text:        pollVotesToText(poll, db),
				Description: strings.Join(poll.Variants, " / "),
			})
			results[idx].SetResultID(strconv.Itoa(idx))
			results[idx].SetParseMode(tele.ModeMarkdownV2)

			results[idx].SetReplyMarkup(prepareInlineVotes(poll))
		}
		return c.Answer(&tele.QueryResponse{
			Results:   results,
			CacheTime: 20,
		})
	})

	b.Handle(tele.OnCallback, func(c tele.Context) error {
		data := strings.Split(c.Callback().Data, "|")
		if len(data) != 3 {
			return c.Respond()
		}
		authorID := c.Callback().Sender.ID

		var process Poll
		txGet := db.Where("id::text = ?", data[1]).First(&process)
		if txGet.Error != nil && !errors.Is(txGet.Error, gorm.ErrRecordNotFound) {
			fmt.Println(">>", txGet.Error)
			return c.Respond(&tele.CallbackResponse{Text: "Got error, try again"})
		}
		if txGet.RowsAffected == 0 {
			return c.Respond(&tele.CallbackResponse{Text: "Unknown poll"})
		}

		variant, err := strconv.Atoi(data[2])
		if err != nil {
			fmt.Println(">>", err)
			return c.Respond(&tele.CallbackResponse{Text: "Got error, try again"})
		}

		var vote Vote
		txGet = db.Where(&Vote{
			PollID: uuid.MustParse(data[1]),
			UserID: authorID,
		}).First(&vote)
		if txGet.Error != nil && !errors.Is(txGet.Error, gorm.ErrRecordNotFound) {
			fmt.Println(">>", txGet.Error)
			return c.Respond(&tele.CallbackResponse{Text: "Got error, try again"})
		}

		if txGet.RowsAffected != 0 {
			db.Delete(&vote)
			if vote.Variant == variant {
				if _, err := b.Edit(c.Callback(), pollVotesToText(process, db), &tele.SendOptions{ParseMode: tele.ModeMarkdownV2, ReplyMarkup: prepareInlineVotes(process)}); err != nil && err != tele.ErrTrueResult {
					fmt.Println(">>", err)
					return c.Respond(&tele.CallbackResponse{Text: "Got error, try again"})
				}
				return c.Respond(&tele.CallbackResponse{Text: "Vote removed"})
			}
		}

		tx := db.Create(&Vote{
			PollID:   uuid.MustParse(data[1]),
			UserID:   authorID,
			Variant:  variant,
			Username: c.Callback().Sender.Username,
		})
		if tx.Error != nil {
			fmt.Println(">>", tx.Error)
			return c.Respond(&tele.CallbackResponse{Text: "Got error, try again"})
		}

		fmt.Println(">> Message", c.Callback().Message)
		fmt.Println(">> Message ID", c.Callback().MessageID)

		if _, err := b.Edit(c.Callback(), pollVotesToText(process, db), &tele.SendOptions{ParseMode: tele.ModeMarkdownV2, ReplyMarkup: prepareInlineVotes(process)}); err != nil && err != tele.ErrTrueResult {

			fmt.Println(">>", err)
			return c.Respond(&tele.CallbackResponse{Text: "Got error, try again"})
		}
		return c.Respond(&tele.CallbackResponse{Text: "Vote accepted"})
	})

	b.Handle(tele.OnText, func(c tele.Context) error {
		if strings.HasPrefix(c.Text(), pollPrefixCmd) {
			return pollInfo(c, db)
		}

		authorID := c.Message().Sender.ID
		var process Poll
		txGet := db.Where("author_id = ? AND state NOT IN ?", authorID, []string{string(statusOpen), string(statusClose)}).Take(&process)
		if txGet.Error != nil && !errors.Is(txGet.Error, gorm.ErrRecordNotFound) {
			fmt.Println(">>", err)
			return c.Send("Got error, try again")
		}
		if txGet.RowsAffected == 0 {
			return c.Send("You have no new poll, send /create to start new poll")
		}

		message := ""
		switch process.State {
		case statusNew:
			process.Question.String = c.Text()
			process.Question.Valid = true
			process.State = statusText
			message = "Good, now send me first answer"
		case statusText:
			process.Variants = append(process.Variants, c.Text())
			fmt.Println(process.Variants)
			message = "Good, now send me another answer"
		}
		if db.Save(&process).Error != nil {
			fmt.Println(">>", err)
			message = "Got error, try again"
		}
		return c.Send(message)
	})

	b.Start()
}
