// Copyright (c) 2017 Sagar Gubbi. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package views

import (
	"github.com/s-gv/orangeforum/models"
	"github.com/s-gv/orangeforum/models/db"
	"github.com/s-gv/orangeforum/static"
	"github.com/s-gv/orangeforum/templates"
	"html/template"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

var IndexHandler = UA(func(w http.ResponseWriter, r *http.Request, sess Session) {
	if r.URL.Path != "/" {
		ErrNotFoundHandler(w, r)
		return
	}

	type Group struct {
		Name     string
		Desc     string
		IsSticky int
	}
	groups := []Group{}
	rows := db.Query(`SELECT name, description, is_sticky FROM groups WHERE is_closed=0 ORDER BY is_sticky DESC, RANDOM() LIMIT 25;`)
	for rows.Next() {
		groups = append(groups, Group{})
		g := &groups[len(groups)-1]
		rows.Scan(&g.Name, &g.Desc, &g.IsSticky)
		g.Desc = censor(g.Desc)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	sort.Slice(groups, func(i, j int) bool { return groups[i].IsSticky > groups[j].IsSticky })

	type Topic struct {
		ID          string
		Title       string
		GroupName   string
		OwnerName   string
		CreatedDate string
		NumComments int
	}
	topics := []Topic{}
	trows := db.Query(`SELECT topics.id, topics.title, topics.num_comments, topics.created_date, topics.is_deleted, topics.is_closed, groups.name, groups.is_closed, users.username FROM topics INNER JOIN groups ON topics.groupid=groups.id INNER JOIN users ON topics.userid=users.id ORDER BY topics.created_date DESC LIMIT 20;`)
	for trows.Next() {
		t := Topic{}
		var cDate int64
		var isTopicDeleted, isTopicClosed, isGroupClosed bool
		trows.Scan(&t.ID, &t.Title, &t.NumComments, &cDate, &isTopicDeleted, &isTopicClosed, &t.GroupName, &isGroupClosed, &t.OwnerName)
		t.CreatedDate = timeAgoFromNow(time.Unix(cDate, 0))
		t.Title = censor(t.Title)
		if !isTopicDeleted && !isTopicClosed && !isGroupClosed {
			topics = append(topics, t)
		}
	}
	templates.Render(w, "index.html", map[string]interface{}{
		"Common":                readCommonData(r, sess),
		"GroupCreationDisabled": models.Config(models.GroupCreationDisabled) == "1",
		"HeaderMsg":             models.Config(models.HeaderMsg),
		"Groups":                groups,
		"Topics":                topics,
	})
})

var AdminIndexHandler = A(func(w http.ResponseWriter, r *http.Request, sess Session) {
	if !sess.IsUserSuperAdmin() {
		ErrForbiddenHandler(w, r)
		return
	}

	linkID := r.PostFormValue("linkid")

	if r.Method == "POST" && linkID == "" {
		forumName := strings.TrimSpace(r.PostFormValue("forum_name"))
		headerMsg := strings.TrimSpace(r.PostFormValue("header_msg"))
		censoredWords := r.PostFormValue("censored_words")
		loginMsg := strings.TrimSpace(r.PostFormValue("login_msg"))
		signupMsg := strings.TrimSpace(r.PostFormValue("signup_msg"))
		signupDisabled := "0"
		groupCreationDisabled := "0"
		imageUploadEnabled := "0"
		allowGroupSubscription := "0"
		allowTopicSubscription := "0"
		readOnlyMode := "0"
		dataDir := r.PostFormValue("data_dir")
		bodyAppendage := r.PostFormValue("body_appendage")
		defaultFromEmail := r.PostFormValue("default_from_mail")
		smtpHost := r.PostFormValue("smtp_host")
		smtpPort := r.PostFormValue("smtp_port")
		smtpUser := r.PostFormValue("smtp_user")
		smtpPass := r.PostFormValue("smtp_pass")
		if r.PostFormValue("signup_disabled") != "" {
			signupDisabled = "1"
		}
		if r.PostFormValue("group_creation_disabled") != "" {
			groupCreationDisabled = "1"
		}
		if r.PostFormValue("image_upload_enabled") != "" {
			imageUploadEnabled = "1"
		}
		if r.PostFormValue("allow_group_subscription") != "" {
			allowGroupSubscription = "1"
		}
		if r.PostFormValue("allow_topic_subscription") != "" {
			allowTopicSubscription = "1"
		}
		if r.PostFormValue(models.ReadOnlyMode) != "" {
			readOnlyMode = "1"
		}
		if dataDir != "" {
			if dataDir[len(dataDir)-1] != '/' {
				dataDir = dataDir + "/"
			}
		}

		errMsg := ""
		if forumName == "" {
			errMsg = "Forum name is empty."
		}

		if errMsg == "" {
			models.WriteConfig(models.ForumName, forumName)
			models.WriteConfig(models.HeaderMsg, headerMsg)
			models.WriteConfig(models.LoginMsg, loginMsg)
			models.WriteConfig(models.SignupMsg, signupMsg)
			models.WriteConfig(models.SignupDisabled, signupDisabled)
			models.WriteConfig(models.CensoredWords, censoredWords)
			models.WriteConfig(models.GroupCreationDisabled, groupCreationDisabled)
			models.WriteConfig(models.ImageUploadEnabled, imageUploadEnabled)
			models.WriteConfig(models.AllowGroupSubscription, allowGroupSubscription)
			models.WriteConfig(models.AllowTopicSubscription, allowTopicSubscription)
			models.WriteConfig(models.ReadOnlyMode, readOnlyMode)
			models.WriteConfig(models.DataDir, dataDir)
			models.WriteConfig(models.BodyAppendage, bodyAppendage)
			models.WriteConfig(models.DefaultFromMail, defaultFromEmail)
			models.WriteConfig(models.SMTPHost, smtpHost)
			models.WriteConfig(models.SMTPPort, smtpPort)
			models.WriteConfig(models.SMTPUser, smtpUser)
			models.WriteConfig(models.SMTPPass, smtpPass)
			sess.SetFlashMsg("Update successful.")
		} else {
			sess.SetFlashMsg(errMsg)
		}
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	if r.Method == "POST" && linkID != "" {
		name := r.PostFormValue("name")
		URL := r.PostFormValue("url")
		content := r.PostFormValue("content")
		if linkID == "new" {
			if name != "" && (URL != "" || content != "") {
				db.Exec(`INSERT INTO extranotes(name, URL, content, created_date, updated_date) VALUES(?, ?, ?, ?, ?);`, name, URL, content, time.Now().Unix(), time.Now().Unix())
			} else {
				sess.SetFlashMsg("Enter an external URL or type some content for the footer link.")
			}
		} else {
			if r.PostFormValue("submit") == "Delete" {
				db.Exec(`DELETE FROM extranotes WHERE id=?;`, linkID)
			} else {
				db.Exec(`UPDATE extranotes SET name=?, URL=?, content=?, updated_date=? WHERE id=?;`, name, URL, content, int64(time.Now().Unix()), linkID)
			}

		}
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	rows := db.Query(`SELECT id, name, URL, content FROM extranotes;`)
	var extraNotes []ExtraNote
	for rows.Next() {
		var extraNote ExtraNote
		rows.Scan(&extraNote.ID, &extraNote.Name, &extraNote.URL, &extraNote.Content)
		extraNotes = append(extraNotes, extraNote)
	}

	templates.Render(w, "adminindex.html", map[string]interface{}{
		"Common":      readCommonData(r, sess),
		"Config":      models.ConfigAllVals(),
		"ExtraNotes":  extraNotes,
		"NumUsers":    models.NumUsers(),
		"NumGroups":   models.NumGroups(),
		"NumTopics":   models.NumTopics(),
		"NumComments": models.NumComments(),
	})
})

var NoteHandler = UA(func(w http.ResponseWriter, r *http.Request, sess Session) {
	id := r.FormValue("id")

	row := db.QueryRow(`SELECT name, URL, content, created_date, updated_date FROM extranotes WHERE id=?;`, id)
	var e ExtraNote
	var cDate int64
	var uDate int64
	if err := row.Scan(&e.Name, &e.URL, &e.Content, &cDate, &uDate); err == nil {
		e.CreatedDate = time.Unix(cDate, 0)
		e.UpdatedDate = time.Unix(uDate, 0)
		if e.URL == "" {
			templates.Render(w, "extranote.html", map[string]interface{}{
				"Common":      readCommonData(r, sess),
				"Name":        e.Name,
				"UpdatedDate": e.UpdatedDate,
				"Content":     template.HTML(e.Content),
			})
			return
		} else {
			http.Redirect(w, r, e.URL, http.StatusSeeOther)
			return
		}
	}
	ErrNotFoundHandler(w, r)
})

func FaviconHandler(w http.ResponseWriter, r *http.Request) {
	defer ErrServerHandler(w, r)
	dataDir := models.Config(models.DataDir)
	if dataDir != "" {
		http.ServeFile(w, r, dataDir+"favicon.ico")
		return
	}
	ErrNotFoundHandler(w, r)
}

func StyleHandler(w http.ResponseWriter, r *http.Request) {
	defer ErrServerHandler(w, r)
	w.Header().Set("Content-Type", "text/css")
	w.Header().Set("Cache-Control", "max-age=31536000, public")
	io.WriteString(w, static.StyleSrc)
}

func ScriptHandler(w http.ResponseWriter, r *http.Request) {
	defer ErrServerHandler(w, r)
	w.Header().Set("Content-Type", "text/javascript")
	w.Header().Set("Cache-Control", "max-age=31536000, public")
	io.WriteString(w, static.ScriptSrc)
}

func ImageHandler(w http.ResponseWriter, r *http.Request) {
	defer ErrServerHandler(w, r)
	dataDir := models.Config(models.DataDir)
	if dataDir != "" {
		http.ServeFile(w, r, dataDir+r.FormValue("name"))
		return
	}
	ErrNotFoundHandler(w, r)
}

func TestHandler(w http.ResponseWriter, r *http.Request) {
	defer ErrServerHandler(w, r)
	sess := OpenSession(w, r)
	sess.SetFlashMsg("hi there")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
