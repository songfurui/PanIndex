package service

import (
	"PanIndex/Util"
	"PanIndex/config"
	"PanIndex/entity"
	"PanIndex/jobs"
	"PanIndex/model"
	"errors"
	"github.com/bluele/gcache"
	"github.com/eddieivan01/nic"
	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func GetFilesByPath(account entity.Account, path, pwd string) map[string]interface{} {
	if path == "" {
		path = "/"
	}
	result := make(map[string]interface{})
	list := []entity.FileNode{}
	defer func() {
		if p := recover(); p != nil {
			log.Errorln(p)
		}
	}()
	result["HasReadme"] = false
	if account.Mode == "native" {
		//列出文件夹相对路径
		rootPath := account.RootId
		fullPath := filepath.Join(rootPath, path)
		if Util.FileExist(fullPath) {
			if Util.IsDirectory(fullPath) {
				//是目录
				// 读取该文件夹下所有文件
				fileInfos, err := ioutil.ReadDir(fullPath)
				//默认按照目录，时间倒序排列
				sort.Slice(fileInfos, func(i, j int) bool {
					d1 := 0
					if fileInfos[i].IsDir() {
						d1 = 1
					}
					d2 := 0
					if fileInfos[j].IsDir() {
						d2 = 1
					}
					if d1 > d2 {
						return true
					} else if d1 == d2 {
						return fileInfos[i].ModTime().After(fileInfos[j].ModTime())
					} else {
						return false
					}
				})
				if err != nil {
					panic(err.Error())
				} else {
					for _, fileInfo := range fileInfos {
						fileId := filepath.Join(fullPath, fileInfo.Name())
						// 当前文件是隐藏文件(以.开头)则不显示
						if Util.IsHiddenFile(fileInfo.Name()) {
							continue
						}
						if fileInfo.Name() == "README.md" {
							result["HasReadme"] = true
							result["ReadmeContent"] = Util.ReadStringByFile(fileId)
						}
						//指定隐藏的文件或目录过滤
						if config.GloablConfig.HideFileId != "" {
							listSTring := strings.Split(config.GloablConfig.HideFileId, ",")
							sort.Strings(listSTring)
							i := sort.SearchStrings(listSTring, fileId)
							if i < len(listSTring) && listSTring[i] == fileId {
								continue
							}
						}
						fileType := Util.GetMimeType(fileInfo)
						// 实例化FileNode
						file := entity.FileNode{
							FileId:     fileId,
							IsFolder:   fileInfo.IsDir(),
							FileName:   fileInfo.Name(),
							FileSize:   int64(fileInfo.Size()),
							SizeFmt:    Util.FormatFileSize(int64(fileInfo.Size())),
							FileType:   strings.TrimLeft(filepath.Ext(fileInfo.Name()), "."),
							Path:       filepath.Join(path, fileInfo.Name()),
							MediaType:  fileType,
							LastOpTime: time.Unix(fileInfo.ModTime().Unix(), 0).Format("2006-01-02 15:04:05"),
						}
						// 添加到切片中等待json序列化
						list = append(list, file)
					}
				}
				result["isFile"] = false
				result["HasPwd"] = false
				PwdDirIds := config.GloablConfig.PwdDirId
				for _, pdi := range strings.Split(PwdDirIds, ",") {
					if strings.Split(pdi, ":")[0] == fullPath && pwd != strings.Split(pdi, ":")[1] {
						result["HasPwd"] = true
						result["FileId"] = fullPath
					}
				}
			} else {
				//是文件
				fileInfo, err := os.Stat(fullPath)
				if err != nil {
					panic(err.Error())
				} else {
					fileType := Util.GetMimeType(fileInfo)
					file := entity.FileNode{
						FileId:     fullPath,
						IsFolder:   fileInfo.IsDir(),
						FileName:   fileInfo.Name(),
						FileSize:   int64(fileInfo.Size()),
						SizeFmt:    Util.FormatFileSize(int64(fileInfo.Size())),
						FileType:   strings.TrimLeft(filepath.Ext(fileInfo.Name()), "."),
						Path:       filepath.Join(path, fileInfo.Name()),
						MediaType:  fileType,
						LastOpTime: time.Unix(fileInfo.ModTime().Unix(), 0).Format("2006-01-02 15:04:05"),
					}
					// 添加到切片中等待json序列化
					list = append(list, file)
				}
				result["isFile"] = true
			}
		}
	} else {
		model.SqliteDb.Raw("select * from file_node where parent_path=? and `delete`=0 and hide = 0 and account_id=?", path, account.Id).Find(&list)
		result["isFile"] = false
		if len(list) == 0 {
			result["isFile"] = true
			model.SqliteDb.Raw("select * from file_node where path = ? and is_folder = 0 and `delete`=0 and hide = 0 and account_id=? limit 1", path, account.Id).Find(&list)
		} else {
			readmeFile := entity.FileNode{}
			model.SqliteDb.Raw("select * from file_node where parent_path=? and file_name=? and `delete`=0 and account_id=?", path, "README.md", account.Id).Find(&readmeFile)
			if !readmeFile.IsFolder && readmeFile.FileName == "README.md" {
				result["HasReadme"] = true
				result["ReadmeContent"] = Util.ReadStringByUrl(GetDownlaodUrl(account, readmeFile), readmeFile.FileId)
			}
		}
		result["HasPwd"] = false
		fileNode := entity.FileNode{}
		model.SqliteDb.Raw("select * from file_node where path = ? and is_folder = 1 and `delete`=0 and account_id = ?", path, account.Id).First(&fileNode)
		PwdDirIds := config.GloablConfig.PwdDirId
		for _, pdi := range strings.Split(PwdDirIds, ",") {
			if pdi != "" {
				if strings.Split(pdi, ":")[0] == fileNode.FileId && pwd != strings.Split(pdi, ":")[1] {
					result["HasPwd"] = true
					result["FileId"] = fileNode.FileId
				}
			}
		}
	}
	result["List"] = list
	result["Path"] = path
	if path == "/" {
		result["HasParent"] = false
	} else {
		result["HasParent"] = true
	}
	result["ParentPath"] = PetParentPath(path)
	if account.Mode == "cloud189" || account.Mode == "native" {
		result["SurportFolderDown"] = true
	} else {
		result["SurportFolderDown"] = false
	}
	return result
}

func SearchFilesByKey(account entity.Account, key string) map[string]interface{} {
	result := make(map[string]interface{})
	list := []entity.FileNode{}
	defer func() {
		if p := recover(); p != nil {
			log.Errorln(p)
		}
	}()
	if account.Mode == "native" {
		list = Util.FileSearch(account.RootId, "", key)
	} else {
		model.SqliteDb.Raw("select * from file_node where file_name like ? and `delete`=0 and hide = 0 and account_id=?", "%"+key+"%", account.Id).Find(&list)
	}
	result["List"] = list
	result["Path"] = "/"
	result["HasParent"] = false
	result["ParentPath"] = PetParentPath("/")
	if account.Mode == "cloud189" || account.Mode == "native" {
		result["SurportFolderDown"] = true
	} else {
		result["SurportFolderDown"] = false
	}
	return result
}

func GetDownlaodUrl(account entity.Account, fileNode entity.FileNode) string {
	if account.Mode == "cloud189" {
		return Util.GetDownlaodUrl(account.Id, fileNode.FileIdDigest)
	} else if account.Mode == "teambition" {
		if Util.TeambitionSessions[account.Id].IsPorject {
			return Util.GetTeambitionProDownUrl("www", account.Id, fileNode.FileId)
		} else {
			return Util.GetTeambitionDownUrl(account.Id, fileNode.FileId)
		}
	} else if account.Mode == "teambition-us" {
		if Util.TeambitionSessions[account.Id].IsPorject {
			return Util.GetTeambitionProDownUrl("us", account.Id, fileNode.FileId)
		} else {
			//国际版暂时没有个人文件
		}
	} else if account.Mode == "aliyundrive" {
		return Util.AliGetDownloadUrl(account.Id, fileNode.FileId)
	} else if account.Mode == "native" {
	}
	return ""
}

func GetDownlaodMultiFiles(accountId, fileId string) string {
	return Util.GetDownlaodMultiFiles(accountId, fileId)
}

func GetPath(accountId, fileId string) string {
	fileNode := entity.FileNode{}
	model.SqliteDb.Raw("select * from file_node where account_id = ? and file_id = ? and delete = 0 limit 1", accountId, fileId).Find(&fileNode)
	return fileNode.Path
}

func PetParentPath(p string) string {
	if p == "/" {
		return ""
	} else {
		s := ""
		ss := strings.Split(p, "/")
		for i := 0; i < len(ss)-1; i++ {
			if ss[i] != "" {
				s += "/" + ss[i]
			}
		}
		if s == "" {
			s = "/"
		}
		return s
	}
}

//获取查询游标start
func GetPageStart(pageNo, pageSize int) int {
	if pageNo < 1 {
		pageNo = 1
	}
	if pageSize < 1 {
		pageSize = 0
	}
	return (pageNo - 1) * pageSize
}

//获取总页数
func GetTotalPage(totalCount, pageSize int) int {
	if pageSize == 0 {
		return 0
	}
	if totalCount%pageSize == 0 {
		return totalCount / pageSize
	} else {
		return totalCount/pageSize + 1
	}
}

//刷新目录缓存
func UpdateFolderCache(account entity.Account) {
	Util.GC = gcache.New(10).LRU().Build()
	model.SqliteDb.Delete(entity.FileNode{})
	if account.Mode == "cloud189" {
		Util.Cloud189GetFiles(account.Id, account.RootId, account.RootId, "")
	} else if account.Mode == "teambition" {
		Util.TeambitionGetFiles(account.Id, account.RootId, account.RootId, "/")
	} else if account.Mode == "native" {
	}
}

//刷新登录cookie
func RefreshCookie(account entity.Account) {
	if account.Mode == "cloud189" {
		Util.Cloud189Login(account.Id, account.User, account.Password)
	} else if account.Mode == "teambition" {
		Util.TeambitionLogin(account.Id, account.User, account.Password)
	} else if account.Mode == "native" {
	}
}
func IsDirectory(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func IsFile(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func GetConfig() entity.Config {
	c := entity.Config{}
	accounts := []entity.Account{}
	damagou := entity.Damagou{}
	model.SqliteDb.Raw("select * from config where 1=1 limit 1").Find(&c)
	model.SqliteDb.Raw("select * from account order by `default`desc").Find(&accounts)
	model.SqliteDb.Raw("select * from damagou where 1-1 limit 1").Find(&damagou)
	c.Accounts = accounts
	c.Damagou = damagou
	config.GloablConfig = c
	return c
}

func SaveConfig(config map[string]interface{}) {
	if config["accounts"] == nil {
		//基本配置
		model.SqliteDb.Table("config").Where("1 = 1").Updates(config)
		if config["hide_file_id"] != nil {
			hideFiles := config["hide_file_id"].(string)
			if hideFiles != "" {
				model.SqliteDb.Table("file_node").Where("1 = 1").Update("hide", 0)
				for _, hf := range strings.Split(hideFiles, ",") {
					model.SqliteDb.Table("file_node").Where("file_id = ?", hf).Update("hide", 1)
				}
			}
		}
	} else {
		//账号信息
		for _, account := range config["accounts"].([]interface{}) {
			mode := account.(map[string]interface{})["Mode"]
			ID := ""
			if account.(map[string]interface{})["id"] != nil && account.(map[string]interface{})["id"] != "" {
				old := entity.Account{}
				model.SqliteDb.Table("account").Where("id = ?", account.(map[string]interface{})["id"]).First(&old)
				if account.(map[string]interface{})["password"] == "" {
					delete(account.(map[string]interface{}), "password")
				}
				//更新网盘账号
				model.SqliteDb.Table("account").Where("id = ?", account.(map[string]interface{})["id"]).Updates(account.(map[string]interface{}))
				if mode != old.Mode {
					delete(Util.CLoud189Sessions, old.Id)
					delete(Util.TeambitionSessions, old.Id)
					if mode == "cloud189" {
						Util.CLoud189Sessions[old.Id] = nic.Session{}
					} else if mode == "teambition" {
						Util.TeambitionSessions[old.Id] = entity.Teambition{nic.Session{}, "", "", "", "", "", false}
					} else if mode == "teambition-us" {
						Util.TeambitionSessions[old.Id] = entity.Teambition{nic.Session{}, "", "", "", "", "", false}
					}
				}
				ID = old.Id
			} else {
				//添加网盘账号
				id := uuid.NewV4().String()
				ID = id
				account.(map[string]interface{})["id"] = id
				account.(map[string]interface{})["status"] = 1
				account.(map[string]interface{})["cookie_status"] = 1
				account.(map[string]interface{})["files_count"] = 0
				model.SqliteDb.Table("account").Create(account.(map[string]interface{}))
				if mode == "cloud189" {
					Util.CLoud189Sessions[id] = nic.Session{}
				} else if mode == "teambition" {
					Util.TeambitionSessions[id] = entity.Teambition{nic.Session{}, "", "", "", "", "", false}
				} else if mode == "teambition-us" {
					Util.TeambitionSessions[id] = entity.Teambition{nic.Session{}, "", "", "", "", "", false}
				}
			}
			ac := entity.Account{}
			model.SqliteDb.Table("account").Where("id=?", ID).Take(&ac)
			go jobs.SyncInit(ac)
		}
	}
	go GetConfig()
	//其他（打码狗）
}
func DeleteAccount(id string) {
	//删除账号对应节点数据
	model.SqliteDb.Where("account_id = ?", id).Delete(entity.FileNode{})
	//删除账号数据
	var a entity.Account
	a.Id = id
	model.SqliteDb.Model(entity.Account{}).Delete(a)
	go GetConfig()
	delete(Util.CLoud189Sessions, id)
	delete(Util.TeambitionSessions, id)
}
func GetAccount(id string) entity.Account {
	account := entity.Account{}
	model.SqliteDb.Where("id = ?", id).First(&account)
	return account
}
func SetDefaultAccount(id string) {
	accountMap := make(map[string]interface{})
	accountMap["default"] = 0
	model.SqliteDb.Model(entity.Account{}).Where("1=1").Updates(accountMap)
	accountMap["default"] = 1
	model.SqliteDb.Table("account").Where("id=?", id).Updates(accountMap)
	go GetConfig()
}
func EnvToConfig() {
	config := os.Getenv("PAN_INDEX_CONFIG")
	if config != "" {
		//从环境变量写入数据库
		c := make(map[string]interface{})
		jsoniter.UnmarshalFromString(config, &c)
		if os.Getenv("PORT") != "" {
			c["port"] = os.Getenv("PORT")
		}
		c["damagou"] = nil
		delete(c, "damagou")
		model.SqliteDb.Where("1 = 1").Delete(&entity.Damagou{})
		model.SqliteDb.Where("1 = 1").Delete(&entity.Account{})
		//model.SqliteDb.Where("1 = 1").Delete(&entity.FileNode{})
		for _, account := range c["accounts"].([]interface{}) {
			//添加网盘账号
			account.(map[string]interface{})["status"] = 1
			account.(map[string]interface{})["files_count"] = 0
			model.SqliteDb.Table("account").Create(account.(map[string]interface{}))
		}
		delete(c, "accounts")
		SaveConfig(c)
	}
}
func Upload(accountId, path string, c *gin.Context) string {
	form, _ := c.MultipartForm()
	files := form.File["uploadFile"]
	dbFile := entity.FileNode{}
	account := entity.Account{}
	result := model.SqliteDb.Raw("select * from account where id=?", accountId).Take(&account)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "指定的账号不存在"
	}
	if account.Mode == "native" {
		p := filepath.FromSlash(account.RootId + path)
		if !Util.FileExist(p) {
			return "指定的目录不存在"
		}
		//服务器本地模式
		for _, file := range files {
			log.Debugf("开始上传文件：%s，大小：%d", file.Filename, file.Size)
			t1 := time.Now()
			p = filepath.FromSlash(account.RootId + path + "/" + file.Filename)
			c.SaveUploadedFile(file, p)
			log.Debugf("文件：%s，上传成功，耗时：%s", file.Filename, Util.ShortDur(time.Now().Sub(t1)))
		}
		return "上传成功"
	} else {
		if path == "/" {
			result = model.SqliteDb.Raw("select * from file_node where parent_path=? and `delete`=0 and account_id=? limit 1", path, accountId).Take(&dbFile)
		} else {
			result = model.SqliteDb.Raw("select * from file_node where path=? and `delete`=0 and account_id=?", path, accountId).Take(&dbFile)
		}
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "指定的目录不存在"
		} else {
			fileId := dbFile.FileId
			if path == "/" {
				fileId = dbFile.ParentId
			}
			if account.Mode == "teambition" && !Util.TeambitionSessions[accountId].IsPorject {
				//teambition 个人文件上传
				Util.TeambitionUpload(accountId, fileId, files)
			} else if account.Mode == "teambition" && Util.TeambitionSessions[accountId].IsPorject {
				//teambition 项目文件上传
				Util.TeambitionProUpload("", accountId, fileId, files)
			} else if account.Mode == "teambition-us" && Util.TeambitionSessions[accountId].IsPorject {
				//teambition-us 项目文件上传
				Util.TeambitionProUpload("us", accountId, fileId, files)
			} else if account.Mode == "cloud189" {
				//天翼云盘文件上传
				Util.Cloud189UploadFiles(accountId, fileId, files)
			} else if account.Mode == "aliyundrive" {
				//阿里云盘文件上传
				Util.AliUpload(accountId, fileId, files)
			}
			return "上传成功"
		}
	}
}

func Async(accountId, path string) string {
	account := entity.Account{}
	result := model.SqliteDb.Raw("select * from account where id=?", accountId).Take(&account)
	dbFile := entity.FileNode{}
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return "指定的账号不存在"
	}
	if account.Mode == "native" {
		return "无需刷新"
	} else {
		if path == "/" {
			result = model.SqliteDb.Raw("select * from file_node where parent_path=? and `delete`=0 and account_id=? limit 1", path, accountId).Take(&dbFile)
		} else {
			result = model.SqliteDb.Raw("select * from file_node where path=? and `delete`=0 and account_id=?", path, accountId).Take(&dbFile)
		}
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "指定的目录不存在"
		} else {
			fileId := dbFile.FileId
			if path == "/" {
				fileId = dbFile.ParentId
			}
			if account.Mode == "teambition" && !Util.TeambitionSessions[accountId].IsPorject {
				//teambition 个人文件
				Util.TeambitionGetFiles(account.Id, fileId, fileId, path)
			} else if account.Mode == "teambition" && Util.TeambitionSessions[accountId].IsPorject {
				//teambition 项目文件
				Util.TeambitionGetProjectFiles("www", account.Id, fileId, path)
			} else if account.Mode == "teambition-us" && Util.TeambitionSessions[accountId].IsPorject {
				//teambition-us 项目文件
				Util.TeambitionGetProjectFiles("us", account.Id, fileId, path)
			} else if account.Mode == "cloud189" {
				Util.Cloud189GetFiles(account.Id, fileId, fileId, path)
			} else if account.Mode == "aliyundrive" {
				Util.AliGetFiles(account.Id, fileId, fileId, path)
			}
			refreshFileNodes(account.Id, fileId)
			return "刷新成功"
		}
	}
}
func refreshFileNodes(accountId, fileId string) {
	tmpList := []entity.FileNode{}
	list := []entity.FileNode{}
	model.SqliteDb.Raw("select * from file_node where parent_id=? and `delete`=0 and account_id=?", fileId, accountId).Find(&tmpList)
	getAllNodes(&tmpList, &list)
	for _, fn := range list {
		model.SqliteDb.Where("id=?", fn.Id).Delete(entity.FileNode{})
	}
	model.SqliteDb.Table("file_node").Where("account_id=?", accountId).Update("delete", 0)
}

func getAllNodes(tmpList, list *[]entity.FileNode) {
	for _, fn := range *tmpList {
		tmpList = &[]entity.FileNode{}
		model.SqliteDb.Raw("select * from file_node where parent_id=? and `delete`=0", fn.FileId).Find(&tmpList)
		*list = append(*list, fn)
		if len(*tmpList) != 0 {
			getAllNodes(tmpList, list)
		}
	}
}
