package api

import (
	"github.com/revel/revel"
	//	"encoding/json"
	. "github.com/leanote/leanote/app/lea"
	//	"gopkg.in/mgo.v2/bson"
	//	"github.com/leanote/leanote/app/lea/netutil"
	"github.com/leanote/leanote/app/info"
	//	"io/ioutil"
	"os"
	//	"strconv"
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// 文件操作, 图片, 头像上传, 输出

type ApiFile struct {
	ApiBaseContrller
}

/*
// 协作时复制图片到owner
func (c ApiFile) CopyImage(userId, fileId, toUserId string) revel.Result {
	re := info.NewRe()

	re.Ok, re.Id = fileService.CopyImage(userId, fileId, toUserId)

	return c.RenderJSON(re)
}

// get all images by userId with page
func (c ApiFile) GetImages(albumId, key string, page int) revel.Result {
	imagesPage := fileService.ListImagesWithPage(c.getUserId(), albumId, key, page, 12)
	re := info.NewRe()
	re.Ok = true
	re.Item = imagesPage
	return c.RenderJSON(re)
}

func (c ApiFile) UpdateImageTitle(fileId, title string) revel.Result {
	re := info.NewRe()
	re.Ok = fileService.UpdateImageTitle(c.getUserId(), fileId, title)
	return c.RenderJSON(re)
}

func (c ApiFile) DeleteImage(fileId string) revel.Result {
	re := info.NewRe()
	re.Ok, re.Msg = fileService.DeleteImage(c.getUserId(), fileId)
	return c.RenderJSON(re)
}

*/

//-----------

// 输出image
// [OK]
func (c ApiFile) GetImage(fileId string) revel.Result {
	path := fileService.GetFile(c.getUserId(), fileId) // 得到路径
	if path == "" {
		return c.RenderText("")
	}
	fn := revel.BasePath + "/" + strings.TrimLeft(path, "/")
	file, _ := os.Open(fn)
	return c.RenderFile(file, revel.Inline) // revel.Attachment
}

// 下载附件
// [OK]
func (c ApiFile) GetAttach(fileId string) revel.Result {
	attach := attachService.GetAttach(fileId, c.getUserId()) // 得到路径
	path := attach.Path
	if path == "" {
		return c.RenderText("No Such File")
	}
	fn := revel.BasePath + "/" + strings.TrimLeft(path, "/")
	file, _ := os.Open(fn)
	return c.RenderBinary(file, attach.Title, revel.Attachment, time.Now()) // revel.Attachment
}

// 下载所有附件
// [OK]
func (c ApiFile) GetAllAttachs(noteId string) revel.Result {
	if !IsObjectId(noteId) {
		return c.RenderText("")
	}
	note := noteService.GetNoteById(noteId)
	if note.NoteId == "" {
		return c.RenderText("")
	}
	// 得到文件列表
	attachs := attachService.ListAttachs(noteId, c.getUserId())
	if attachs == nil || len(attachs) == 0 {
		return c.RenderText("")
	}

	dir := revel.BasePath + "/files/attach_all"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return c.RenderText("")
	}

	// 随机文件名, 避免并发的同名打包互相覆盖
	tmpPath := dir + "/" + NewGuid() + ".tar.gz"
	if err := writeAttachsTar(tmpPath, attachs); err != nil {
		os.Remove(tmpPath)
		return c.RenderText("")
	}
	defer os.Remove(tmpPath)

	file, err := os.Open(tmpPath)
	if err != nil {
		return c.RenderText("")
	}
	defer file.Close()

	return c.RenderBinary(file, "attachs.tar.gz", revel.Attachment, time.Now()) // revel.Attachment
}

// 将附件打包为 tar.gz, 归档内文件名只保留 base 名
func writeAttachsTar(toPath string, attachs []info.Attach) error {
	fw, err := os.Create(toPath)
	if err != nil {
		return err
	}
	gw := gzip.NewWriter(fw)
	tw := tar.NewWriter(gw)

	for i, attach := range attachs {
		name := path.Base(strings.Replace(attach.Title, "\\", "/", -1))
		if name == "" || name == "." || name == ".." {
			name = fmt.Sprintf("attach-%d", i+1)
		}

		fn := revel.BasePath + "/" + strings.TrimLeft(attach.Path, "/")
		fr, err := os.Open(fn)
		if err != nil {
			continue // 单个附件丢失时跳过
		}
		fileInfo, err := fr.Stat()
		if err != nil || fileInfo.IsDir() {
			fr.Close()
			continue
		}

		err = tw.WriteHeader(&tar.Header{
			Name:    name,
			Size:    fileInfo.Size(),
			Mode:    int64(fileInfo.Mode()),
			ModTime: fileInfo.ModTime(),
		})
		if err == nil {
			_, err = io.Copy(tw, fr)
		}
		fr.Close()
		if err != nil {
			tw.Close()
			gw.Close()
			fw.Close()
			return err
		}
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		fw.Close()
		return err
	}
	if err := gw.Close(); err != nil {
		fw.Close()
		return err
	}
	return fw.Close()
}
