// Copyright (c) 2017 Sagar Gubbi. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package views

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/s-gv/orangeforum/models"
	"github.com/s-gv/orangeforum/models/db"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

type CommonData struct {
	CSRF              string
	Msg               string
	UserName          string
	IsSuperAdmin      bool
	IsNotification    bool
	ForumName         string
	PageTitle         string
	CurrentURL        template.URL
	BodyAppendage     string
	IsGroupSubAllowed bool
	IsTopicSubAllowed bool
	ExtraNotesShort   []ExtraNote
}

type ExtraNote struct {
	ID          int
	Name        string
	Content     string
	URL         string
	CreatedDate time.Time
	UpdatedDate time.Time
}

var linkRe *regexp.Regexp
var italicRe *regexp.Regexp
var boldRe *regexp.Regexp
var codeRe *regexp.Regexp
var oldCodeRe *regexp.Regexp
var quoteRe *regexp.Regexp
var censorRe *regexp.Regexp

var censored string

func init() {
	linkRe = regexp.MustCompile("https?://([A-Za-z0-9\\-]+\\.[A-Za-z0-9\\-\\.]+|localhost)(:[0-9]+)?[a-zA-Z0-9@:%_\\+\\.~#?&/=;\\-]*[a-zA-Z0-9@:%_\\+~#?&/=;\\-]")
	italicRe = regexp.MustCompile("\\*([^\\*\n]+)\\*")
	boldRe = regexp.MustCompile("\\*\\*([^\\*\n]+)\\*\\*")
	codeRe = regexp.MustCompile("(?:^|\n)```.*\n(?s:(.+))\n```(?:$|\n)")
	oldCodeRe = regexp.MustCompile("(?:^|\n)    ([^\n]+)")
	quoteRe = regexp.MustCompile("((?:^|\n)>*)[ ]*(\\S[^\n]*)")
}

func ErrServerHandler(w http.ResponseWriter, r *http.Request) {
	if r := recover(); r != nil {
		log.Printf("[INFO] Recovered from panic: %s\n[INFO] Debug stack: %s\n", r, debug.Stack())
		http.Error(w, "Internal server error. This event has been logged.", http.StatusInternalServerError)
	}
}

func ErrNotFoundHandler(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}

func ErrForbiddenHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "403 Forbidden", http.StatusForbidden)
}

func UA(handler func(w http.ResponseWriter, r *http.Request, sess Session)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		defer ErrServerHandler(w, r)
		sess := OpenSession(w, r)
		if r.Method == "POST" && r.PostFormValue("csrf") != sess.CSRFToken {
			ErrForbiddenHandler(w, r)
			return
		}
		//log.Printf("[INFO] Request: %s\n", r.URL)
		handler(w, r, sess)
	}
}

func A(handler func(w http.ResponseWriter, r *http.Request, sess Session)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		defer ErrServerHandler(w, r)
		sess := OpenSession(w, r)
		if r.Method == "POST" && r.PostFormValue("csrf") != sess.CSRFToken {
			ErrForbiddenHandler(w, r)
			return
		}
		if !sess.UserID.Valid {
			redirectURL := r.URL.Path
			if r.URL.RawQuery != "" {
				redirectURL += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, "/login?next="+url.QueryEscape(redirectURL), http.StatusSeeOther)
			return
		}
		if r.Method == "POST" && models.Config(models.ReadOnlyMode) != "0" && !sess.IsUserSuperAdmin() {
			http.Error(w, "Forum is in read-only mode.", http.StatusForbidden)
			return
		}
		//log.Printf("[INFO] Request: %s\n", r.URL)
		handler(w, r, sess)
	}
}

func timeAgoFromNow(t time.Time) string {
	diff := time.Now().Sub(t)
	if diff.Hours() > 24 {
		return strconv.Itoa(int(diff.Hours()/24)) + " days ago"
	} else if diff.Hours() >= 2 {
		return strconv.Itoa(int(diff.Hours())) + " hours ago"
	} else {
		return strconv.Itoa(int(diff.Minutes())) + " minutes ago"
	}
	return ""
}

func validateName(name string) error {
	if len(name) == 0 {
		return errors.New("Name cannot be blank.")
	}
	hasSpecial := false
	for _, ch := range name {
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && ch != '_' && ch != '-' && (ch < '0' || ch > '9') {
			hasSpecial = true
		}
	}
	if hasSpecial {
		return errors.New("Name can contain only english alphabets, numbers, hyphens, and underscore.")
	}
	return nil
}

func formatComment(comment string) template.HTML {
	comment = strings.Replace(comment, "\r", "", -1)

	comment = template.HTMLEscapeString(comment)

	if strings.Contains(comment, "```") {
		// Newer version -- Markdown style backtick marked code blocks
		comment = codeRe.ReplaceAllString(comment, "<pre>$1</pre>")
	} else {
		// Older version -- SO style 4-space indented code blocks
		comment = oldCodeRe.ReplaceAllString(comment, "<pre>$1</pre>")
		comment = strings.Replace(comment, "</pre><pre>", "\n", -1)
	}

	comment = strings.Replace(comment, "\n\n", "</p><p>", -1)
	comment = strings.Replace(comment, "\n", "<br>", -1)

	comment = boldRe.ReplaceAllString(comment, "<b>$1</b>")
	comment = italicRe.ReplaceAllString(comment, "<em>$1</em>")

	comment = linkRe.ReplaceAllString(comment, "<a href=\"$0\">$0</a>")

	formatted := "<p>" + comment + "</p>"
	return template.HTML(censor(formatted))
}

func formatReply(quotedUser string, quoteContent string) string {
	quoteContent = strings.Replace(quoteContent, "\r", "", -1)
	quoteContent = codeRe.ReplaceAllString(quoteContent, "\n$1\n")
	if quoteContent[0] == '\n' {
		quoteContent = quoteContent[1:]
	}
	quoteContent = quoteRe.ReplaceAllString(quoteContent, "$1> $2")
	quoteContent = "```\n" + quotedUser + " wrote:\n" + quoteContent + "\n```\n"
	return quoteContent
}

func censor(content string) string {
	cWords := models.Config(models.CensoredWords)
	if cWords != censored {
		censored = cWords
		cWordList := strings.Split(censored, ",")
		if len(cWordList) > 0 {
			r := "(?i:"
			for i, w := range cWordList {
				r = r + strings.TrimSpace(w)
				if i < len(cWordList)-1 {
					r = r + "|"
				}
			}
			r = r + ")"
			censorRe = regexp.MustCompile(r)
		} else {
			censorRe = nil
		}
	}
	if censorRe != nil {
		return censorRe.ReplaceAllString(content, "****")
	}
	return content
}

func saveImage(r *http.Request) string {
	imageName := ""
	if dataDir := models.Config(models.DataDir); dataDir != "" {
		r.ParseMultipartForm(32 * 1024 * 1024)
		file, handler, err := r.FormFile("img")
		if err == nil {
			defer file.Close()
			if handler.Filename != "" {
				ext := strings.ToLower(filepath.Ext(handler.Filename))
				if ext == ".jpg" || ext == ".png" || ext == ".jpeg" {
					fileName := randSeq(32) + ext
					f, err := os.OpenFile(dataDir+fileName, os.O_WRONLY|os.O_CREATE, 0666)
					if err == nil {
						defer f.Close()
						io.Copy(f, file)
						imageName = fileName
					} else {
						log.Panicf("[ERROR] Error writing file: %s\n", err)
					}
				}
			}
		}
	} else {
		log.Panicf("[ERROR] Unable to accept file upload. DataDir not configured.\n")
	}
	return imageName
}

func validatePasswd(passwd string, passwdConfirm string) error {
	if len(passwd) < 8 || len(passwd) > 40 {
		return errors.New("Password should have 8-40 characters.")
	}
	if passwd != passwdConfirm {
		return errors.New("Passwords don't match.")
	}
	return nil
}

func randSeq(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		log.Panicf("[ERROR] Unable to generate random number: %s\n", err.Error())
	}
	return base64.URLEncoding.EncodeToString(b)
}

func readCommonData(r *http.Request, sess Session) CommonData {
	userName := ""
	isSuperAdmin := false
	if sess.UserID.Valid {
		r := db.QueryRow(`SELECT username, is_superadmin FROM users WHERE id=?;`, sess.UserID)
		r.Scan(&userName, &isSuperAdmin)
	}
	currentURL := "/"
	if r.URL.Path != "" {
		currentURL = r.URL.Path
		if r.URL.RawQuery != "" {
			currentURL = currentURL + "?" + r.URL.RawQuery
		}
	}

	pmNotification := false
	if sess.UserID.Valid {
		var tmp string
		if err := db.QueryRow(`SELECT id FROM messages WHERE toid=? AND is_read=?`, sess.UserID, false).Scan(&tmp); err == nil {
			pmNotification = true
		}
	}

	rows := db.Query(`SELECT id, name FROM extranotes;`)
	var extraNotes []ExtraNote
	for rows.Next() {
		var extraNote ExtraNote
		rows.Scan(&extraNote.ID, &extraNote.Name)
		extraNotes = append(extraNotes, extraNote)
	}

	return CommonData{
		CSRF:              sess.CSRFToken,
		Msg:               sess.FlashMsg(),
		UserName:          userName,
		IsSuperAdmin:      isSuperAdmin,
		IsNotification:    pmNotification,
		ForumName:         models.Config(models.ForumName),
		CurrentURL:        template.URL(url.QueryEscape(currentURL)),
		IsGroupSubAllowed: models.Config(models.AllowGroupSubscription) != "0",
		IsTopicSubAllowed: models.Config(models.AllowTopicSubscription) != "0",
		BodyAppendage:     models.Config(models.BodyAppendage),
		ExtraNotesShort:   extraNotes,
	}
}
