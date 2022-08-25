package main

import (
	"database/sql"
	"database/sql/driver"
	"github.com/lib/pq"
	"gorm.io/gorm"
	"strconv"
	"time"

	"github.com/google/uuid"
)

type PollState string

const (
	statusNew      PollState = "new"
	statusText     PollState = "text"
	statusVariants PollState = "variants"
	statusOpen     PollState = "open"
	statusClose    PollState = "close"
)

const (
	selectLimit = 10
)

func (p *PollState) Scan(value interface{}) error {
	*p = PollState(value.(string))
	return nil
}

func (p *PollState) Value() (driver.Value, error) {
	return string(*p), nil
}

type Poll struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primarykey"`

	CreateDT time.Time `gorm:"default:now()"`
	UpdateDT time.Time `gorm:"default:now()"`

	AuthorID int64
	Question sql.NullString
	Variants pq.StringArray `gorm:"type:text[]"`
	State    PollState      `gorm:"default:'new'" sql:"type:state"`
}

func (Poll) TableName() string {
	return "votes.t_poll"
}

func pollToTextBase(poll Poll, db *gorm.DB) string {
	message := "Question: *" + escapeString(poll.Question.String) + "*\n\n"
	for _, variant := range poll.Variants {
		message += "\t • " + escapeString(variant) + "\n"
	}
	return message
}

func pollVotesToText(poll Poll, db *gorm.DB) string {
	message := "Question: *" + escapeString(poll.Question.String) + "*\n\n"
	for idx, variant := range poll.Variants {
		message += "\t • " + escapeString(variant) + " – "

		var votes []Vote
		db.Where(map[string]interface{}{"variant": idx, "poll_id": poll.ID}).Find(&votes)

		message += strconv.Itoa(len(votes)) + "\n"

		if len(votes) > 0 {
			for idx, vote := range votes {
				if idx != 0 {
					message += ", "
				}
				var tag string
				if len(vote.Username) != 0 {
					tag = "@" + vote.Username
				} else {
					tag = "[" + vote.Fullname + "](tg://user?id=" + strconv.FormatInt(vote.UserID, 10) + ")"
				}
				message += tag
			}
			message += "\n"
		}
	}
	return message
}

type Vote struct {
	ID uint `gorm:"primarykey"`

	PollID uuid.UUID `gorm:"type:uuid;"`
	UserID int64

	Variant  int
	Username string
	Fullname string
}

func (Vote) TableName() string {
	return "votes.t_vote"
}
