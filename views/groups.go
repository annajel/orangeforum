// Copyright (c) 2017 Sagar Gubbi. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package views

import (
	"github.com/s-gv/orangeforum/models"
	"github.com/s-gv/orangeforum/models/db"
	"github.com/s-gv/orangeforum/templates"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var GroupIndexHandler = UA(func(w http.ResponseWriter, r *http.Request, sess Session) {
	name := r.FormValue("name")
	var groupID, groupDesc, headerMsg string
	if db.QueryRow(`SELECT id, description, header_msg FROM groups WHERE name=?;`, name).Scan(&groupID, &groupDesc, &headerMsg) != nil {
		ErrNotFoundHandler(w, r)
		return
	}

	subToken := ""
	if sess.UserID.Valid {
		db.QueryRow(`SELECT token FROM groupsubscriptions WHERE groupid=? AND userid=?;`, groupID, sess.UserID).Scan(&subToken)
	}

	numTopicsPerPage := 30
	lastTopicDate, err := strconv.ParseInt(r.FormValue("ltd"), 10, 64)
	if err != nil {
		lastTopicDate = 0
	}

	type Topic struct {
		ID          int
		Title       string
		IsDeleted   bool
		IsClosed    bool
		Owner       string
		NumComments int
		CreatedDate string
		cDateUnix   int64
	}
	var topics []Topic
	var rows *db.Rows
	if lastTopicDate == 0 {
		rows = db.Query(`SELECT topics.id, topics.title, topics.is_deleted, topics.is_closed, topics.num_comments, topics.created_date, users.username FROM topics INNER JOIN users ON topics.userid = users.id AND topics.groupid=? ORDER BY topics.is_sticky DESC, topics.activity_date DESC LIMIT ?;`, groupID, numTopicsPerPage)
	} else {
		rows = db.Query(`SELECT topics.id, topics.title, topics.is_deleted, topics.is_closed, topics.num_comments, topics.created_date, users.username FROM topics INNER JOIN users ON topics.userid = users.id AND topics.groupid=? AND topics.is_sticky=0 AND topics.created_date < ? ORDER BY topics.activity_date DESC LIMIT ?;`, groupID, lastTopicDate, numTopicsPerPage)
	}
	for rows.Next() {
		t := Topic{}
		rows.Scan(&t.ID, &t.Title, &t.IsDeleted, &t.IsClosed, &t.NumComments, &t.cDateUnix, &t.Owner)
		t.CreatedDate = timeAgoFromNow(time.Unix(t.cDateUnix, 0))
		t.Title = censor(t.Title)
		topics = append(topics, t)
	}

	isSuperAdmin := false
	isAdmin := false
	isMod := false
	if sess.IsUserValid() {
		db.QueryRow(`SELECT is_superadmin FROM users WHERE id=?;`, sess.UserID).Scan(&isSuperAdmin)
		var tmp string
		isAdmin = db.QueryRow(`SELECT id FROM admins WHERE groupid=? AND userid=?;`, groupID, sess.UserID).Scan(&tmp) == nil
		isMod = db.QueryRow(`SELECT id FROM mods WHERE groupid=? AND userid=?;`, groupID, sess.UserID).Scan(&tmp) == nil
	}

	if len(topics) >= numTopicsPerPage {
		lastTopicDate = topics[len(topics)-1].cDateUnix
	} else {
		lastTopicDate = 0
	}

	commonData := readCommonData(r, sess)
	commonData.PageTitle = name

	templates.Render(w, "groupindex.html", map[string]interface{}{
		"Common":        commonData,
		"GroupName":     name,
		"GroupDesc":     censor(groupDesc),
		"GroupID":       groupID,
		"HeaderMsg":     censor(headerMsg),
		"SubToken":      subToken,
		"Topics":        topics,
		"IsMod":         isMod,
		"IsAdmin":       isAdmin,
		"IsSuperAdmin":  isSuperAdmin,
		"LastTopicDate": lastTopicDate,
	})
})

var GroupEditHandler = A(func(w http.ResponseWriter, r *http.Request, sess Session) {
	if models.Config(models.GroupCreationDisabled) == "1" {
		ErrForbiddenHandler(w, r)
		return
	}
	commonData := readCommonData(r, sess)

	userName := commonData.UserName

	groupID := r.FormValue("id")
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("desc"))
	headerMsg := strings.TrimSpace(r.FormValue("header_msg"))
	isSticky := r.FormValue("is_sticky") != ""
	isPrivate := r.FormValue("is_private") != ""
	isDeleted := false
	mods := strings.Split(r.FormValue("mods"), ",")
	for i, mod := range mods {
		mods[i] = strings.TrimSpace(mod)
	}
	admins := strings.Split(r.FormValue("admins"), ",")
	for i, admin := range admins {
		admins[i] = strings.TrimSpace(admin)
	}
	if len(admins) == 1 && admins[0] == "" {
		admins[0] = userName
	}
	action := r.FormValue("action")

	if groupID != "" {
		if !models.IsUserGroupAdmin(strconv.Itoa(int(sess.UserID.Int64)), groupID) && !commonData.IsSuperAdmin {
			ErrForbiddenHandler(w, r)
			return
		}
	}

	if r.Method == "POST" {
		if action == "Create" {
			if len(name) < 3 || len(name) > 40 {
				sess.SetFlashMsg("Group name should have 3-40 characters.")
				http.Redirect(w, r, "/groups/edit", http.StatusSeeOther)
				return
			}
			if censored := censor(name); censored != name {
				sess.SetFlashMsg("Fix group name: " + censored)
				http.Redirect(w, r, "/groups/edit", http.StatusSeeOther)
				return
			}
			if len(desc) > 160 {
				sess.SetFlashMsg("Group description should have less than 160 characters.")
				http.Redirect(w, r, "/groups/edit", http.StatusSeeOther)
				return
			}
			if len(headerMsg) > 160 {
				sess.SetFlashMsg("Announcement should have less than 160 characters.")
				http.Redirect(w, r, "/groups/edit", http.StatusSeeOther)
				return
			}
			if err := validateName(name); err != nil {
				sess.SetFlashMsg(err.Error())
				http.Redirect(w, r, "/groups/edit", http.StatusSeeOther)
				return
			}
			if len(admins) > 32 || len(mods) > 32 {
				sess.SetFlashMsg("Number of admins/mods should no more than 32.")
				http.Redirect(w, r, "/groups/edit", http.StatusSeeOther)
				return
			}
			db.Exec(`INSERT INTO groups(name, description, header_msg, is_sticky, is_private, created_date, updated_date) VALUES(?, ?, ?, ?, ?, ?, ?);`, name, desc, headerMsg, isSticky, isPrivate, time.Now().Unix(), time.Now().Unix())
			groupID := models.ReadGroupIDByName(name)
			for _, mod := range mods {
				if mod != "" {
					models.CreateGroupMod(mod, groupID)
				}
			}
			for _, admin := range admins {
				if admin != "" {
					models.CreateGroupAdmin(admin, groupID)
				}
			}
			http.Redirect(w, r, "/groups?name="+name, http.StatusSeeOther)
		} else if action == "Update" {
			if len(name) < 3 || len(name) > 40 {
				sess.SetFlashMsg("Group name should have 3-40 characters.")
				http.Redirect(w, r, "/groups/edit?id="+groupID, http.StatusSeeOther)
				return
			}
			if censored := censor(name); censored != name {
				sess.SetFlashMsg("Fix group name: " + censored)
				http.Redirect(w, r, "/groups/edit?id="+groupID, http.StatusSeeOther)
				return
			}
			if len(desc) > 160 {
				sess.SetFlashMsg("Group description should have less than 160 characters.")
				http.Redirect(w, r, "/groups/edit?id="+groupID, http.StatusSeeOther)
				return
			}
			if len(headerMsg) > 160 {
				sess.SetFlashMsg("Announcement should have less than 160 characters.")
				http.Redirect(w, r, "/groups/edit?id="+groupID, http.StatusSeeOther)
				return
			}
			if err := validateName(name); err != nil {
				sess.SetFlashMsg(err.Error())
				http.Redirect(w, r, "/groups/edit?id="+groupID, http.StatusSeeOther)
				return
			}
			if len(admins) > 32 || len(mods) > 32 {
				sess.SetFlashMsg("Number of admins/mods should no more than 32.")
				http.Redirect(w, r, "/groups/edit?id="+groupID, http.StatusSeeOther)
				return
			}
			isUserSuperAdmin := false
			db.QueryRow(`SELECT is_superadmin FROM users WHERE id=?;`, sess.UserID).Scan(&isUserSuperAdmin)
			if !isUserSuperAdmin {
				db.QueryRow(`SELECT is_sticky FROM groups WHERE id=?;`, groupID).Scan(&isSticky)
			}
			db.Exec(`UPDATE groups SET name=?, description=?, header_msg=?, is_sticky=?, is_private=?, updated_date=? WHERE id=?;`, name, desc, headerMsg, isSticky, isPrivate, time.Now().Unix(), groupID)
			db.Exec(`DELETE FROM mods WHERE groupid=?;`, groupID)
			db.Exec(`DELETE FROM admins WHERE groupid=?;`, groupID)
			for _, mod := range mods {
				if mod != "" {
					models.CreateGroupMod(mod, groupID)
				}
			}
			for _, admin := range admins {
				if admin != "" {
					models.CreateGroupAdmin(admin, groupID)
				}
			}
			http.Redirect(w, r, "/groups?name="+name, http.StatusSeeOther)
		} else if action == "Delete" {
			db.Exec(`UPDATE groups SET is_closed=1 WHERE id=?;`, groupID)
			http.Redirect(w, r, "/groups/edit?id="+groupID, http.StatusSeeOther)
		} else if action == "Undelete" {
			db.Exec(`UPDATE groups SET is_closed=0 WHERE id=?;`, groupID)
			http.Redirect(w, r, "/groups/edit?id="+groupID, http.StatusSeeOther)
		}
		return
	}

	if groupID != "" {
		// Open to edit
		db.QueryRow(`SELECT name, description, header_msg, is_sticky, is_private, is_closed FROM groups WHERE id=?;`, groupID).Scan(
			&name, &desc, &headerMsg, &isSticky, &isPrivate, &isDeleted,
		)
		mods = models.ReadMods(groupID)
		admins = models.ReadAdmins(groupID)
	}

	templates.Render(w, "groupedit.html", map[string]interface{}{
		"Common":    readCommonData(r, sess),
		"ID":        groupID,
		"GroupName": name,
		"Desc":      desc,
		"HeaderMsg": headerMsg,
		"IsSticky":  isSticky,
		"IsPrivate": isPrivate,
		"IsDeleted": isDeleted,
		"Mods":      strings.Join(mods, ", "),
		"Admins":    strings.Join(admins, ", "),
	})
})

var GroupSubscribeHandler = A(func(w http.ResponseWriter, r *http.Request, sess Session) {
	groupID := r.FormValue("id")
	if models.Config(models.AllowGroupSubscription) == "0" {
		ErrForbiddenHandler(w, r)
		return
	}
	var groupName string
	if db.QueryRow(`SELECT name FROM groups WHERE id=?;`, groupID).Scan(&groupName) != nil {
		ErrNotFoundHandler(w, r)
		return
	}
	if r.Method == "POST" {
		var tmp string
		if db.QueryRow(`SELECT id FROM groupsubscriptions WHERE userid=? AND groupid=?;`, sess.UserID, groupID).Scan(&tmp) != nil {
			db.Exec(`INSERT INTO groupsubscriptions(userid, groupid, token, created_date) VALUES(?, ?, ?, ?);`,
				sess.UserID, groupID, randSeq(64), time.Now().Unix())
		}
	}
	http.Redirect(w, r, "/groups?name="+groupName, http.StatusSeeOther)
})

var GroupUnsubscribeHandler = UA(func(w http.ResponseWriter, r *http.Request, sess Session) {
	token := r.FormValue("token")
	var groupID, groupName string
	if db.QueryRow(`SELECT groupid FROM groupsubscriptions WHERE token=?;`, token).Scan(&groupID) != nil {
		ErrNotFoundHandler(w, r)
		return
	}
	db.QueryRow(`SELECT name FROM groups WHERE id=?;`, groupID).Scan(&groupName)
	if r.Method == "POST" {
		db.Exec(`DELETE FROM groupsubscriptions WHERE token=?;`, token)
		if r.PostFormValue("noredirect") != "" {
			w.Write([]byte("Unsubscribed."))
		} else {
			http.Redirect(w, r, "/groups?name="+groupName, http.StatusSeeOther)
		}
		return
	}
	w.Write([]byte(`<!DOCTYPE html><html><head>
	<meta http-equiv="Content-Type" content="text/html; charset=utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1"></head>
	<body><form action="/groups/unsubscribe" method="POST">
	Unsubscribe from ` + groupName + `?
	<input type="hidden" name="token" value=` + token + `>
	<input type="hidden" name="csrf" value="` + sess.CSRFToken + `">
	<input type="hidden" name="noredirect" value="1">
	<input type="submit" value="Unsubscribe">
	</form></body></html>`))
})
