package main

import (
	"PanIndex/Util"
	"PanIndex/boot"
	"PanIndex/config"
	"PanIndex/entity"
	"PanIndex/jobs"
	"PanIndex/service"
	"flag"
	"fmt"
	"github.com/bluele/gcache"
	"github.com/gin-gonic/gin"
	"github.com/gobuffalo/packr/v2"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"github.com/unrolled/secure"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//var configPath = flag.String("config", "config.json", "配置文件config.json的路径")
var Host = flag.String("host", "", "绑定host，默认为0.0.0.0")
var Port = flag.String("port", "", "绑定port，默认为8080")
var Debug = flag.Bool("debug", false, "调试模式，设置为true可以输出更多日志")
var DataPath = flag.String("data_path", "data", "数据存储目录，默认程序同级目录")
var CertFile = flag.String("cert_file", "", "/path/to/test.pem")
var KeyFile = flag.String("key_file", "", "/path/to/test.key")
var GC = gcache.New(100).LRU().Build()

func main() {
	flag.Parse()
	boot.Start(*Host, *Port, *Debug, *DataPath)
	r := gin.New()
	r.Use(gin.Logger())
	//	staticBox := packr.NewBox("./static")
	r.SetHTMLTemplate(initTemplates())
	//r.LoadHTMLGlob("templates/*	")
	//	r.LoadHTMLFiles("templates/**")
	//	r.Static("/static", "./static")
	//	r.StaticFS("/static", staticBox)
	//r.StaticFile("/favicon-cloud189.ico", "./static/img/favicon-cloud189.ico")
	initStaticBox(r)
	//声明一个路由
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		method := c.Request.Method
		_, ad := c.GetQuery("admin")
		if strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/api/public") {
			requestToken := c.Query("token")
			if requestToken != config.GloablConfig.ApiToken {
				message := "Invalid api token"
				c.String(http.StatusOK, message)
				return
			}
		}
		if path == "/api/public/downloadMultiFiles" {
			//文件夹下载
			downloadMultiFiles(c)
		} else if method == http.MethodGet && path == "/api/updateFolderCache" {
			message := ""
			for _, account := range config.GloablConfig.Accounts {
				if account.Mode == "native" {
					log.Infoln("[API请求]目录缓存刷新 >> 当前为本地模式，无需刷新")
				} else {
					go updateCaches(account)
					log.Infoln("[API请求]目录缓存刷新 >> 请求刷新")
				}
			}
			message = "Cache update successful"
			c.String(http.StatusOK, message)
		} else if method == http.MethodGet && path == "/api/refreshCookie" {
			message := ""
			for _, account := range config.GloablConfig.Accounts {
				if account.Mode == "native" {
					log.Infoln("[API请求]cookie刷新刷新 >> 当前为本地模式，无需刷新")
				} else {
					go refreshCookie(account)
					log.Infoln("[API请求]cookie刷新 >> 请求刷新")
				}
			}
			message = "Cookie refresh successful"
			c.String(http.StatusOK, message)
		} else if method == http.MethodGet && path == "/api/shareToDown" {
			shareToDown(c)
		} else if method == http.MethodPost && path == "/api/admin/save" {
			adminSave(c)
		} else if path == "/api/admin/deleteAccount" {
			adminDeleteAccount(c)
		} else if path == "/api/admin/updateCache" {
			updateCache(c)
		} else if path == "/api/admin/updateCookie" {
			updateCookie(c)
		} else if path == "/api/admin/setDefaultAccount" {
			setDefaultAccount(c)
		} else if path == "/api/admin/config" {
			getConfig(c)
		} else if path == "/api/admin/envToConfig" {
			envToConfig(c)
		} else if path == "/api/admin/upload" {
			upload(c)
		} else if ad {
			admin(c)
		} else {
			isForbidden := true
			host := c.Request.Host
			referer, err := url.Parse(c.Request.Referer())
			if err != nil {
				log.Warningln(err)
			}
			if referer != nil && referer.Host != "" {
				if referer.Host == host {
					//站内，自动通过
					isForbidden = false
				} else if referer.Host != host && len(config.GloablConfig.OnlyReferrer) > 0 {
					//外部引用，并且设置了防盗链，需要进行判断
					for _, rf := range strings.Split(config.GloablConfig.OnlyReferrer, ",") {
						if rf == referer.Host {
							isForbidden = false
							break
						}
					}
				} else {
					isForbidden = false
				}
			} else {
				isForbidden = false
			}
			if isForbidden == true {
				c.String(http.StatusForbidden, "403 Hotlink Forbidden")
				return
			} else {
				k, s := c.GetQuery("search")
				if s {
					search(c, k)
				} else {
					index(c)
				}
			}
		}
	})
	log.Infoln("程序启动成功")
	if *CertFile != "" && *KeyFile != "" {
		//开启https
		r.Use(TlsHandler(config.GloablConfig.Port))
		r.RunTLS(fmt.Sprintf("%s:%d", config.GloablConfig.Host, config.GloablConfig.Port), *CertFile, *KeyFile)
	} else {
		r.Run(fmt.Sprintf("%s:%d", config.GloablConfig.Host, config.GloablConfig.Port))
	}
}

func TlsHandler(port int) gin.HandlerFunc {
	return func(c *gin.Context) {
		secureMiddleware := secure.New(secure.Options{
			SSLRedirect: true,
			SSLHost:     ":" + strconv.Itoa(port),
		})
		err := secureMiddleware.Process(c.Writer, c.Request)
		// If there was an error, do not continue.
		if err != nil {
			return
		}
		c.Next()
	}
}

func initTemplates() *template.Template {
	themes := [6]string{"mdui", "mdui-light", "mdui-dark", "classic", "bootstrap", "materialdesign"}
	box := packr.New("templates", "./templates")
	data, _ := box.FindString("pan/admin/login.html")
	tmpl := template.New("pan/admin/login.html")
	tmpl.Parse(data)
	data, _ = box.FindString("pan/admin/index.html")
	tmpl.New("pan/admin/index.html").Parse(data)
	for _, theme := range themes {
		tmpName := strings.Join([]string{"pan/", "/index.html"}, theme)
		tmpFile := strings.ReplaceAll(tmpName, "-dark", "")
		tmpFile = strings.ReplaceAll(tmpFile, "-light", "")
		data, _ = box.FindString(tmpFile)
		if Util.FileExist("./templates/" + tmpFile) {
			s, _ := ioutil.ReadFile("./templates/" + tmpFile)
			data = string(s)
		}
		tmpl.New(tmpName).Funcs(template.FuncMap{"unescaped": unescaped}).Parse(data)
	}
	return tmpl
}
func initStaticBox(r *gin.Engine) {
	staticBox := packr.New("static", "./static")
	if Util.FileExist("./static") {
		r.Static("/static", "./static")
	} else {
		r.StaticFS("/static", staticBox)
	}
}

func index(c *gin.Context) {
	tmpFile := strings.Join([]string{"pan/", "/index.html"}, config.GloablConfig.Theme)
	pwd := ""
	pwdCookie, err := c.Request.Cookie("dir_pwd")
	if err == nil {
		decodePwd, err := url.QueryUnescape(pwdCookie.Value)
		if err != nil {
			log.Warningln(err)
		}
		pwd = decodePwd
	}
	pathName := c.Request.URL.Path
	if pathName != "/" && pathName[len(pathName)-1:] == "/" {
		pathName = pathName[0 : len(pathName)-1]
	}
	index := 0
	DIndex := ""
	if strings.HasPrefix(pathName, "/d_") {
		iStr := Util.GetBetweenStr(pathName, "_", "/")
		index, _ = strconv.Atoi(iStr)
		pathName = strings.ReplaceAll(pathName, "/d_"+iStr, "")
		DIndex = fmt.Sprintf("/d_%d", index)
	} else {
		DIndex = ""
	}
	if len(config.GloablConfig.Accounts) == 0 {
		//未绑定任何账号，跳转到后台进行配置
		c.Redirect(http.StatusFound, "/?admin")
		return
	}
	account := config.GloablConfig.Accounts[index]
	result := service.GetFilesByPath(account, pathName, pwd)
	result["HerokuappUrl"] = config.GloablConfig.HerokuAppUrl
	result["Mode"] = account.Mode
	result["PrePaths"] = Util.GetPrePath(result["Path"].(string))
	result["Title"] = account.Name
	result["Accounts"] = config.GloablConfig.Accounts
	result["DIndex"] = DIndex
	result["AccountId"] = account.Id
	result["Footer"] = config.GloablConfig.Footer
	result["Theme"] = config.GloablConfig.Theme
	result["FaviconUrl"] = config.GloablConfig.FaviconUrl
	fs, ok := result["List"].([]entity.FileNode)
	if ok {
		if len(fs) == 1 && !fs[0].IsFolder && result["isFile"].(bool) {
			//文件
			if account.Mode == "native" {
				c.Writer.Header().Add("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fs[0].FileName))
				c.Writer.Header().Add("Content-Type", "application/octet-stream")
				c.File(fs[0].FileId)
				return
			} else {
				downUrl := service.GetDownlaodUrl(account, fs[0])
				c.Redirect(http.StatusFound, downUrl)
			}
		}
	}
	c.HTML(http.StatusOK, tmpFile, result)
}

func search(c *gin.Context, key string) {
	tmpFile := strings.Join([]string{"pan/", "/index.html"}, config.GloablConfig.Theme)
	pathName := c.Request.URL.Path
	if pathName != "/" && pathName[len(pathName)-1:] == "/" {
		pathName = pathName[0 : len(pathName)-1]
	}
	index := 0
	DIndex := ""
	if strings.HasPrefix(pathName, "/d_") {
		iStr := Util.GetBetweenStr(pathName, "_", "/")
		index, _ = strconv.Atoi(iStr)
		pathName = strings.ReplaceAll(pathName, "/d_"+iStr, "")
		DIndex = fmt.Sprintf("/d_%d", index)
	} else {
		DIndex = ""
	}
	if len(config.GloablConfig.Accounts) == 0 {
		//未绑定任何账号，跳转到后台进行配置
		c.Redirect(http.StatusFound, "/?admin")
		return
	}
	account := config.GloablConfig.Accounts[index]
	result := service.SearchFilesByKey(account, key)
	result["HerokuappUrl"] = config.GloablConfig.HerokuAppUrl
	result["Mode"] = account.Mode
	result["PrePaths"] = Util.GetPrePath(result["Path"].(string))
	result["Title"] = account.Name
	result["Accounts"] = config.GloablConfig.Accounts
	result["DIndex"] = DIndex
	result["AccountId"] = account.Id
	result["Footer"] = config.GloablConfig.Footer
	result["Theme"] = config.GloablConfig.Theme
	result["FaviconUrl"] = config.GloablConfig.FaviconUrl
	result["SearchKey"] = key
	c.HTML(http.StatusOK, tmpFile, result)
}

func downloadMultiFiles(c *gin.Context) {
	fileId := c.Query("fileId")
	accountId := c.Query("accountId")
	account := service.GetAccount(accountId)
	if account.Mode == "native" {
		dp := *DataPath
		if os.Getenv("PAN_INDEX_DATA_PATH") != "" {
			dp = os.Getenv("PAN_INDEX_DATA_PATH")
		}
		if dp == "" {
			dp = "data"
		}
		//src := service.GetPath(accountId, accountId)
		t := time.Now().Format("20060102150405")
		dst := dp + string(filepath.Separator) + t + ".zip"
		Util.Zip(dp+string(filepath.Separator)+t+".zip", fileId)
		c.Writer.Header().Add("Content-Disposition", fmt.Sprintf("attachment; filename=%s", t+".zip"))
		c.Writer.Header().Add("Content-Type", "application/octet-stream")
		c.File(dst)
		return
	} else if account.Mode == "cloud189" {
		downUrl := service.GetDownlaodMultiFiles(accountId, fileId)
		c.JSON(http.StatusOK, gin.H{"redirect_url": downUrl})
	}
}

func updateCaches(account entity.Account) {
	service.UpdateFolderCache(account)
	log.Infoln("[API请求]目录缓存刷新 >> 刷新成功")
}

func refreshCookie(account entity.Account) {
	service.RefreshCookie(account)
	log.Infoln("[API请求]cookie刷新 >> 刷新成功")
}

func shareToDown(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET")
	c.Header("Access-Control-Allow-Headers", "Origin, X-Requested-With, Content-Type, Accept, Authorization")
	c.Header("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers, Cache-Control, Content-Language, Content-Type")
	c.Header("Access-Control-Allow-Credentials", "true")
	url := c.Query("url")
	passCode := c.Query("passCode")
	fileId := c.Query("fileId")
	subFileId := c.Query("subFileId")
	downUrl := Util.Cloud189shareToDown(url, passCode, fileId, subFileId)
	c.String(http.StatusOK, downUrl)
}

func admin(c *gin.Context) {
	logout := c.Query("logout")
	sessionId, error := c.Cookie("sessionId")
	if logout != "" && logout == "true" {
		//退出登录
		GC.Remove(sessionId)
		c.HTML(http.StatusOK, "pan/admin/login.html", gin.H{"Error": true, "Msg": "退出成功", "Theme": config.GloablConfig.Theme})
	} else {
		if c.Request.Method == "GET" {
			if error == nil && sessionId != "" && GC.Has(sessionId) {
				//登录状态跳转首页
				config := service.GetConfig()
				c.HTML(http.StatusOK, "pan/admin/index.html", config)
			} else {
				c.HTML(http.StatusOK, "pan/admin/login.html", gin.H{"Error": false, "Theme": config.GloablConfig.Theme, "FaviconUrl": config.GloablConfig.FaviconUrl})
			}
		} else {
			//登录
			password, _ := c.GetPostForm("password")
			config := service.GetConfig()
			if password == config.AdminPassword {
				//登录成功
				u1 := uuid.NewV4().String()
				c.SetCookie("sessionId", u1, 7*24*60*60, "/", "", false, true)
				GC.SetWithExpire(u1, u1, time.Hour*24*7)
				config := service.GetConfig()
				c.HTML(http.StatusOK, "pan/admin/index.html", config)
			} else {
				c.HTML(http.StatusOK, "pan/admin/login.html", gin.H{"Error": true, "Theme": config.Theme, "FaviconUrl": config.FaviconUrl, "Msg": "密码错误，请重试！"})
			}
		}
	}
}

func adminSave(c *gin.Context) {
	configMap := make(map[string]interface{})
	c.BindJSON(&configMap)
	service.SaveConfig(configMap)
	c.JSON(http.StatusOK, gin.H{"status": 0, "msg": "配置已更新，部分配置重启后生效！"})
}

func adminDeleteAccount(c *gin.Context) {
	id := c.Query("id")
	service.DeleteAccount(id)
	c.JSON(http.StatusOK, gin.H{"status": 0, "msg": "删除成功！"})
}

func getConfig(c *gin.Context) {
	c.JSON(http.StatusOK, service.GetConfig())
}

func updateCache(c *gin.Context) {
	id := c.Query("id")
	account := service.GetAccount(id)
	if account.Status == -1 {
		c.JSON(http.StatusOK, gin.H{"status": 0, "msg": "目录缓存中，请勿重复操作！"})
	} else {
		go jobs.SyncOneAccount(account)
		c.JSON(http.StatusOK, gin.H{"status": 0, "msg": "正在缓存目录，请稍后刷新页面查看缓存结果！"})
	}
}

func updateCookie(c *gin.Context) {
	id := c.Query("id")
	account := service.GetAccount(id)
	if account.CookieStatus == -1 {
		c.JSON(http.StatusOK, gin.H{"status": 0, "msg": "cookie刷新中，请勿重复操作！"})
	} else {
		go jobs.AccountLogin(account)
		c.JSON(http.StatusOK, gin.H{"status": 0, "msg": "正在刷新cookie，请稍后刷新页面查看缓存结果！"})
	}
}

func setDefaultAccount(c *gin.Context) {
	id := c.Query("id")
	service.SetDefaultAccount(id)
	c.JSON(http.StatusOK, gin.H{"status": 0, "msg": "默认账号设置成功！"})
}

func envToConfig(c *gin.Context) {
	service.EnvToConfig()
	c.JSON(http.StatusOK, gin.H{"status": 0, "msg": "同步配置"})
}

func upload(c *gin.Context) {
	accountId := c.PostForm("uploadAccount")
	path := c.PostForm("uploadPath")
	t := c.PostForm("type")
	msg := ""
	if t == "0" {
		msg = service.Upload(accountId, path, c)
	} else if t == "1" {
		service.Async(accountId, path)
		msg = "刷新缓存成功"
	} else if t == "2" {
		service.Upload(accountId, path, c)
		service.Async(accountId, path)
		msg = "上传并刷新成功"
	}
	c.JSON(http.StatusOK, gin.H{"status": 0, "msg": msg})
}

func unescaped(x string) interface{} { return template.HTML(x) }
