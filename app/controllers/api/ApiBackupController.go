package api

import (
	"io/ioutil"
	"os"
	"time"

	"github.com/revel/revel"
	"gopkg.in/mgo.v2/bson"
)

// 全量备份/恢复 API (网页版/桌面版/Android 共用)
type ApiBackup struct {
	ApiBaseContrller
}

func backupRe(ok bool, msg string, item interface{}) map[string]interface{} {
	re := map[string]interface{}{"Ok": ok, "Msg": msg}
	if item != nil {
		re["Item"] = item
	}
	return re
}

// 一键导出全部笔记/图片/附件/笔记本结构
// GET /api/backup/export
func (c ApiBackup) Export() revel.Result {
	userId := c.getUserId()
	if userId == "" || !bson.IsObjectIdHex(userId) {
		return c.RenderJSON(backupRe(false, "notLogin", nil))
	}
	p, err := backupService.ExportZip(userId)
	if err != nil {
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	f, err := os.Open(p)
	if err != nil {
		os.Remove(p)
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	name := "leanote-backup-" + time.Now().Format("20060102-150405") + ".zip"
	// 文件在响应结束后删除
	go func() {
		time.Sleep(10 * time.Minute)
		os.Remove(p)
	}()
	return c.RenderBinary(f, name, revel.Attachment, time.Now())
}

// 一键恢复全部数据
// POST /api/backup/import  (multipart: file)
func (c ApiBackup) Import() revel.Result {
	userId := c.getUserId()
	if userId == "" || !bson.IsObjectIdHex(userId) {
		return c.RenderJSON(backupRe(false, "notLogin", nil))
	}
	files := c.Params.Files["file"]
	if len(files) == 0 {
		return c.RenderJSON(backupRe(false, "noFile", nil))
	}
	src, err := files[0].Open()
	if err != nil {
		return c.RenderJSON(backupRe(false, "noFile", nil))
	}
	defer src.Close()
	tmp, err := ioutil.TempFile("", "leanote-upload-*.zip")
	if err != nil {
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.ReadFrom(src); err != nil {
		tmp.Close()
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	tmp.Close()

	res, err := backupService.ImportZip(userId, tmpPath)
	if err != nil {
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	return c.RenderJSON(backupRe(true, "ok", res))
}

// 管理员: 导出 mongo 整库(备份)
// POST /api/backup/mongoExport
func (c ApiBackup) MongoExport() revel.Result {
	if !c.isAdmin() {
		return c.RenderJSON(backupRe(false, "noPerm", nil))
	}
	p, err := backupService.MongoExportZip()
	if err != nil {
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	f, err := os.Open(p)
	if err != nil {
		os.Remove(p)
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	go func() {
		time.Sleep(10 * time.Minute)
		os.Remove(p)
	}()
	name := "leanote-mongo-" + time.Now().Format("20060102-150405") + ".zip"
	return c.RenderBinary(f, name, revel.Attachment, time.Now())
}

// 管理员: 从 mongo 备份恢复
// POST /api/backup/mongoImport (multipart: file)
func (c ApiBackup) MongoImport() revel.Result {
	if !c.isAdmin() {
		return c.RenderJSON(backupRe(false, "noPerm", nil))
	}
	files := c.Params.Files["file"]
	if len(files) == 0 {
		return c.RenderJSON(backupRe(false, "noFile", nil))
	}
	src, err := files[0].Open()
	if err != nil {
		return c.RenderJSON(backupRe(false, "noFile", nil))
	}
	defer src.Close()
	tmp, err := ioutil.TempFile("", "leanote-mongo-upload-*.zip")
	if err != nil {
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err = tmp.ReadFrom(src); err != nil {
		tmp.Close()
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	tmp.Close()
	if err := backupService.MongoImportZip(tmpPath); err != nil {
		return c.RenderJSON(backupRe(false, err.Error(), nil))
	}
	return c.RenderJSON(backupRe(true, "ok", nil))
}

func (c ApiBackup) isAdmin() bool {
	u := userService.GetUserInfo(c.getUserId())
	return u.Username == leanoteUserId
}
