package service

import (
	"archive/zip"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/leanote/leanote/app/db"
	"github.com/leanote/leanote/app/info"
	. "github.com/leanote/leanote/app/lea"
	"github.com/revel/revel"
	"gopkg.in/mgo.v2/bson"
)

// 全量备份/恢复服务
// 备份包为 zip:
//   leanote-backup.json   清单(笔记本/笔记/文件元数据, 笔记正文原样保存)
//   files/images/*        图片原文件
//   files/attachs/*       附件原文件
//   html/*.html           通用 HTML 格式(可直接阅读/迁移)
//   markdown/*.md         通用 Markdown 格式
// 这样既保留 Leanote 全部数据(格式/字体/时间/标签/结构), 也提供通用格式。

const (
	BackupFormat  = "leanote-backup"
	BackupVersion = 2
)

type BackupNotebook struct {
	NotebookId       string    `json:"notebookId"`
	ParentNotebookId string    `json:"parentNotebookId"`
	Title            string    `json:"title"`
	Seq              int       `json:"seq"`
	IsBlog           bool      `json:"isBlog"`
	IsTrash          bool      `json:"isTrash"`
	CreatedTime      time.Time `json:"createdTime"`
	UpdatedTime      time.Time `json:"updatedTime"`
}

type BackupNote struct {
	NoteId      string    `json:"noteId"`
	NotebookId  string    `json:"notebookId"`
	Title       string    `json:"title"`
	Tags        []string  `json:"tags"`
	IsMarkdown  bool      `json:"isMarkdown"`
	IsBlog      bool      `json:"isBlog"`
	IsTrash     bool      `json:"isTrash"`
	Content     string    `json:"content"`
	CreatedTime time.Time `json:"createdTime"`
	UpdatedTime time.Time `json:"updatedTime"`
}

type BackupFile struct {
	Kind      string    `json:"kind"` // image / attach
	FileId    string    `json:"fileId"`
	NoteId    string    `json:"noteId"`
	Title     string    `json:"title"`
	Type      string    `json:"type"`
	ZipPath   string    `json:"zipPath"`
	CreatedTime time.Time `json:"createdTime"`
}

type BackupManifest struct {
	Format     string           `json:"format"`
	Version    int              `json:"version"`
	App        string           `json:"app"`
	ExportDate string           `json:"exportDate"`
	UserId     string           `json:"userId"`
	Username   string           `json:"username"`
	Email      string           `json:"email"`
	Notebooks  []BackupNotebook `json:"notebooks"`
	Notes      []BackupNote     `json:"notes"`
	Files      []BackupFile     `json:"files"`
}

type BackupService struct{}

var safeNameReg = regexp.MustCompile(`[\\/:*?"<>|\r\n\t]+`)

func backupSafeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Untitled"
	}
	s = safeNameReg.ReplaceAllString(s, "-")
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80])
	}
	return s
}

func zipAddFile(zw *zip.Writer, zipPath, diskPath string) error {
	f, err := os.Open(diskPath)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return nil
	}
	h := &zip.FileHeader{Name: zipPath, Method: zip.Deflate, Modified: st.ModTime()}
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func zipAddBytes(zw *zip.Writer, zipPath string, data []byte) error {
	h := &zip.FileHeader{Name: zipPath, Method: zip.Deflate, Modified: time.Now()}
	w, err := zw.CreateHeader(h)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// 导出全部数据, 返回生成的 zip 临时文件路径
func (this *BackupService) ExportZip(userId string) (string, error) {
	uid := bson.ObjectIdHex(userId)
	manifest := BackupManifest{
		Format:     BackupFormat,
		Version:    BackupVersion,
		App:        "leanote.server",
		ExportDate: time.Now().Format("2006-01-02 15:04:05"),
		UserId:     userId,
	}

	if u := userService.GetUserInfo(userId); u.UserId.Hex() != "" {
		manifest.Username = u.Username
		manifest.Email = u.Email
	}

	// 笔记本(含已删除标记, 以便原样恢复)
	notebooks := []info.Notebook{}
	db.Notebooks.Find(bson.M{"UserId": uid}).Sort("Seq").All(&notebooks)
	for _, nb := range notebooks {
		manifest.Notebooks = append(manifest.Notebooks, BackupNotebook{
			NotebookId:       nb.NotebookId.Hex(),
			ParentNotebookId: nb.ParentNotebookId.Hex(),
			Title:            nb.Title,
			Seq:              nb.Seq,
			IsBlog:           nb.IsBlog,
			IsTrash:          nb.IsTrash,
			CreatedTime:      nb.CreatedTime,
			UpdatedTime:      nb.UpdatedTime,
		})
	}

	// 笔记(含回收站)
	notes := []info.Note{}
	db.Notes.Find(bson.M{"UserId": uid, "IsDeleted": false}).Sort("CreatedTime").All(&notes)
	noteIds := make([]bson.ObjectId, 0, len(notes))
	for _, n := range notes {
		noteIds = append(noteIds, n.NoteId)
	}
	contents := map[string]string{}
	if len(noteIds) > 0 {
		ncs := []info.NoteContent{}
		db.NoteContents.Find(bson.M{"_id": bson.M{"$in": noteIds}}).All(&ncs)
		for _, nc := range ncs {
			contents[nc.NoteId.Hex()] = nc.Content
		}
	}
	for _, n := range notes {
		manifest.Notes = append(manifest.Notes, BackupNote{
			NoteId:      n.NoteId.Hex(),
			NotebookId:  n.NotebookId.Hex(),
			Title:       n.Title,
			Tags:        n.Tags,
			IsMarkdown:  n.IsMarkdown,
			IsBlog:      n.IsBlog,
			IsTrash:     n.IsTrash,
			Content:     contents[n.NoteId.Hex()],
			CreatedTime: n.CreatedTime,
			UpdatedTime: n.UpdatedTime,
		})
	}

	// 图片
	images := []info.File{}
	db.Files.Find(bson.M{"UserId": uid}).All(&images)
	for _, img := range images {
		ext := path.Ext(img.Name)
		if ext == "" {
			ext = path.Ext(img.Path)
		}
		manifest.Files = append(manifest.Files, BackupFile{
			Kind:        "image",
			FileId:      img.FileId.Hex(),
			Title:       img.Title,
			Type:        img.Type,
			ZipPath:     "files/images/" + img.FileId.Hex() + ext,
			CreatedTime: img.CreatedTime,
		})
	}

	// 附件
	attachs := []info.Attach{}
	if len(noteIds) > 0 {
		db.Attachs.Find(bson.M{"NoteId": bson.M{"$in": noteIds}}).All(&attachs)
	}
	for _, at := range attachs {
		ext := path.Ext(at.Name)
		if ext == "" {
			ext = path.Ext(at.Path)
		}
		manifest.Files = append(manifest.Files, BackupFile{
			Kind:        "attach",
			FileId:      at.AttachId.Hex(),
			NoteId:      at.NoteId.Hex(),
			Title:       at.Title,
			Type:        at.Type,
			ZipPath:     "files/attachs/" + at.AttachId.Hex() + ext,
			CreatedTime: at.CreatedTime,
		})
	}

	f, err := ioutil.TempFile("", "leanote-backup-*.zip")
	if err != nil {
		return "", err
	}
	zipPath := f.Name()
	f.Close()
	raw, err := os.OpenFile(zipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(raw)

	// 先用占位写入, 最后再写清单(这样清单里的 zipPath 一定是准确的)
	for i, bf := range manifest.Files {
		var disk string
		if bf.Kind == "image" {
			for _, img := range images {
				if img.FileId.Hex() == bf.FileId {
					disk = revel.BasePath + "/" + img.Path
					break
				}
			}
		} else {
			for _, at := range attachs {
				if at.AttachId.Hex() == bf.FileId {
					disk = revel.BasePath + "/" + at.Path
					break
				}
			}
		}
		_ = i
		if disk == "" {
			manifest.Files[i].ZipPath = ""
			continue
		}
		if err := zipAddFile(zw, bf.ZipPath, disk); err != nil {
			// 文件缺失时保留元数据但标记为空, 不阻断整体导出
			manifest.Files[i].ZipPath = ""
		}
	}

	// 通用格式: HTML / Markdown
	for _, bn := range manifest.Notes {
		base := backupSafeName(bn.Title) + "-" + bn.NoteId
		if bn.IsMarkdown {
			_ = zipAddBytes(zw, "markdown/"+base+".md", []byte(bn.Content))
		} else {
			html := "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>" +
				htmlEscape(bn.Title) + "</title></head><body>" + bn.Content + "</body></html>"
			_ = zipAddBytes(zw, "html/"+base+".html", []byte(html))
		}
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := zipAddBytes(zw, "leanote-backup.json", data); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		raw.Close()
		return "", err
	}
	raw.Close()
	return zipPath, nil
}

func mustOpen(p string) *os.File {
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	return f
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}

type BackupImportResult struct {
	Notebooks int `json:"notebooks"`
	Notes     int `json:"notes"`
	Images    int `json:"images"`
	Attachs   int `json:"attachs"`
}

// 从上传的 zip 恢复全部数据
func (this *BackupService) ImportZip(userId, uploadPath string) (*BackupImportResult, error) {
	zr, err := zip.OpenReader(uploadPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var manifest BackupManifest
	found := false
	zipFiles := map[string]*zip.File{}
	for _, f := range zr.File {
		zipFiles[f.Name] = f
		if f.Name == "leanote-backup.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
				rc.Close()
				return nil, err
			}
			rc.Close()
			found = true
		}
	}
	if !found || manifest.Format != BackupFormat {
		return nil, errNotBackup
	}

	uid := bson.ObjectIdHex(userId)
	res := &BackupImportResult{}

	// 1. 笔记本, 按层级顺序创建
	old2newNb := map[string]bson.ObjectId{}
	pending := append([]BackupNotebook{}, manifest.Notebooks...)
	for len(pending) > 0 {
		next := []BackupNotebook{}
		progress := false
		for _, bn := range pending {
			if bn.ParentNotebookId != "" {
				if _, ok := old2newNb[bn.ParentNotebookId]; !ok {
					next = append(next, bn)
					continue
				}
			}
			nb := info.Notebook{
				UserId: uid,
				Title:  bn.Title,
				Seq:    bn.Seq,
				IsBlog: bn.IsBlog,
			}
			if bn.ParentNotebookId != "" {
				nb.ParentNotebookId = old2newNb[bn.ParentNotebookId]
			}
			ok, newNb := notebookService.AddNotebook(nb)
			if ok {
				old2newNb[bn.NotebookId] = newNb.NotebookId
				res.Notebooks++
			}
			progress = true
		}
		if !progress {
			// 父节点丢失, 挂到根
			for _, bn := range next {
				nb := info.Notebook{UserId: uid, Title: bn.Title, Seq: bn.Seq, IsBlog: bn.IsBlog}
				if ok, newNb := notebookService.AddNotebook(nb); ok {
					old2newNb[bn.NotebookId] = newNb.NotebookId
					res.Notebooks++
				}
			}
			break
		}
		pending = next
	}

	// 默认笔记本(用于找不到归属的笔记)
	var defaultNotebookId bson.ObjectId
	for _, id := range old2newNb {
		defaultNotebookId = id
		break
	}
	if defaultNotebookId == "" {
		nb := info.Notebook{UserId: uid, Title: "导入的笔记", Seq: 0}
		if ok, newNb := notebookService.AddNotebook(nb); ok {
			defaultNotebookId = newNb.NotebookId
			res.Notebooks++
		}
	}

	// 2. 文件, 先落盘并建立旧ID -> 新ID映射
	idMap := map[string]string{} // 旧 fileId/attachId -> 新
	attachDisk := map[string]string{} // 旧 attachId -> 相对路径
	if err := os.MkdirAll(filepath.Join(revel.BasePath, "files"), 0755); err != nil {
		return nil, err
	}
	for _, bf := range manifest.Files {
		if bf.ZipPath == "" {
			continue
		}
		zf, ok := zipFiles[bf.ZipPath]
		if !ok {
			continue
		}
		newId := bson.NewObjectId()
		ext := path.Ext(bf.ZipPath)
		if bf.Kind == "image" {
			dir := "files/" + GetRandomFilePath(userId, newId.Hex()) + "/images"
			absDir := filepath.Join(revel.BasePath, filepath.FromSlash(dir))
			if err := os.MkdirAll(absDir, 0755); err != nil {
				continue
			}
			name := newId.Hex() + ext
			abs := filepath.Join(absDir, name)
			if err := extractZipFile(zf, abs); err != nil {
				continue
			}
			img := info.File{
				FileId:         newId,
				UserId:         uid,
				AlbumId:        bson.ObjectIdHex(DEFAULT_ALBUM_ID),
				Name:           name,
				Title:          bf.Title,
				Size:           GetFilesize(abs),
				Type:           bf.Type,
				Path:           dir + "/" + name,
				IsDefaultAlbum: true,
				CreatedTime:    bf.CreatedTime,
			}
			if img.CreatedTime.IsZero() {
				img.CreatedTime = time.Now()
			}
			if ok, _ := fileService.AddImage(img, "", userId, false); ok {
				idMap[bf.FileId] = newId.Hex()
				res.Images++
			}
		} else {
			dir := "files/" + GetRandomFilePath(userId, newId.Hex()) + "/attachs"
			absDir := filepath.Join(revel.BasePath, filepath.FromSlash(dir))
			if err := os.MkdirAll(absDir, 0755); err != nil {
				continue
			}
			name := newId.Hex() + ext
			abs := filepath.Join(absDir, name)
			if err := extractZipFile(zf, abs); err != nil {
				continue
			}
			idMap[bf.FileId] = newId.Hex()
			res.Attachs++
			attachDisk[bf.FileId] = dir + "/" + name
			// 附件的 NoteId 在笔记创建后回填
		}
	}

	// 3. 笔记
	noteIdMap := map[string]bson.ObjectId{}
	for _, bn := range manifest.Notes {
		nbId := defaultNotebookId
		if id, ok := old2newNb[bn.NotebookId]; ok {
			nbId = id
		}
		content := bn.Content
		// 重写正文中的文件引用
		for oldId, newId := range idMap {
			content = strings.Replace(content, oldId, newId, -1)
		}
		note := info.Note{
			UserId:      uid,
			NotebookId:  nbId,
			Title:       bn.Title,
			Tags:        bn.Tags,
			IsMarkdown:  bn.IsMarkdown,
			IsBlog:      bn.IsBlog,
			CreatedTime: bn.CreatedTime,
			UpdatedTime: bn.UpdatedTime,
		}
		noteContent := info.NoteContent{
			UserId:      uid,
			Content:     content,
			CreatedTime: bn.CreatedTime,
			UpdatedTime: bn.UpdatedTime,
		}
		newNote := noteService.AddNoteAndContent(note, noteContent, uid)
		if newNote.NoteId != "" {
			noteIdMap[bn.NoteId] = newNote.NoteId
			if bn.IsTrash {
				db.UpdateByQMap(db.Notes, bson.M{"_id": newNote.NoteId}, bson.M{"$set": bson.M{"IsTrash": true}})
			}
			res.Notes++
		}
	}

	// 4. 附件记录(需要新笔记ID)
	for _, bf := range manifest.Files {
		if bf.Kind != "attach" || bf.ZipPath == "" {
			continue
		}
		newId, ok := idMap[bf.FileId]
		if !ok {
			continue
		}
		newNoteId, ok := noteIdMap[bf.NoteId]
		if !ok {
			continue
		}
		var realPath, realName string
		realPath = attachDisk[bf.FileId]
		if realPath == "" {
			continue
		}
		realName = filepath.Base(realPath)
		at := info.Attach{
			AttachId:     bson.ObjectIdHex(newId),
			NoteId:       newNoteId,
			UploadUserId: uid,
			Name:         realName,
			Title:        bf.Title,
			Type:         bf.Type,
			Path:         realPath,
			CreatedTime:  bf.CreatedTime,
		}
		if at.CreatedTime.IsZero() {
			at.CreatedTime = time.Now()
		}
		at.Size = GetFilesize(filepath.Join(revel.BasePath, filepath.FromSlash(realPath)))
		attachService.AddAttach(at, true)
	}

	return res, nil
}

var errNotBackup = &backupError{"not a leanote backup package"}

type backupError struct{ s string }

func (e *backupError) Error() string { return e.s }

func extractZipFile(zf *zip.File, abs string) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(abs, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

// 在 files 下查找指定 id 前缀的文件, 返回相对路径和文件名
func findFileById(zipBase string, newId string) (string, string) {
	id := newId
	relDir := ""
	// 附件的落盘位置: files/xx/userId/yy/attachs/<id><ext>
	root := filepath.Join(revel.BasePath, "files")
	var found string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if found != "" {
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, id) {
			found = p
		}
		return nil
	})
	if found == "" {
		return "", ""
	}
	rel, err := filepath.Rel(revel.BasePath, found)
	if err != nil {
		return "", ""
	}
	relDir = filepath.ToSlash(rel)
	return relDir, filepath.Base(found)
}

// 管理员 mongo 导出
func (this *BackupService) MongoExportZip() (string, error) {
	bin, err := exec.LookPath("mongodump")
	if err != nil {
		return "", &backupError{"mongodump not found on server"}
	}
	host := revel.Config.StringDefault("db.host", "127.0.0.1")
	port := revel.Config.StringDefault("db.port", "27017")
	dbname := revel.Config.StringDefault("db.dbname", "leanote")
	user := revel.Config.StringDefault("db.username", "")
	pass := revel.Config.StringDefault("db.password", "")

	dir, err := ioutil.TempDir("", "leanote-mongo-")
	if err != nil {
		return "", err
	}
	args := []string{"--host", host, "--port", port, "-d", dbname, "-o", dir}
	if user != "" {
		args = append(args, "-u", user, "-p", pass, "--authenticationDatabase", "admin")
	}
	cmd := exec.Command(bin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", &backupError{"mongodump failed: " + string(out)}
	}

	zipFile, err := ioutil.TempFile("", "leanote-mongo-*.zip")
	if err != nil {
		return "", err
	}
	zipPath := zipFile.Name()
	zipFile.Close()
	raw, err := os.OpenFile(zipPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(raw)
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil || fi.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(dir, p)
		if e != nil {
			return nil
		}
		_ = zipAddFile(zw, filepath.ToSlash(rel), p)
		return nil
	})
	zw.Close()
	raw.Close()
	os.RemoveAll(dir)
	return zipPath, nil
}

// 管理员 mongo 导入(恢复)
func (this *BackupService) MongoImportZip(uploadPath string) error {
	bin, err := exec.LookPath("mongorestore")
	if err != nil {
		return &backupError{"mongorestore not found on server"}
	}
	dir, err := ioutil.TempDir("", "leanote-mongo-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if err := unzipAll(uploadPath, dir); err != nil {
		return err
	}
	host := revel.Config.StringDefault("db.host", "127.0.0.1")
	port := revel.Config.StringDefault("db.port", "27017")
	dbname := revel.Config.StringDefault("db.dbname", "leanote")
	user := revel.Config.StringDefault("db.username", "")
	pass := revel.Config.StringDefault("db.password", "")
	args := []string{"--host", host, "--port", port, "-d", dbname, "--drop", dir}
	if user != "" {
		args = append(args, "-u", user, "-p", pass, "--authenticationDatabase", "admin")
	}
	cmd := exec.Command(bin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return &backupError{"mongorestore failed: " + string(out)}
	}
	return nil
}

func unzipAll(zipPath, dir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue
		}
		abs := filepath.Join(dir, name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(abs, 0755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return err
		}
		if err := extractZipFile(f, abs); err != nil {
			return err
		}
	}
	return nil
}
