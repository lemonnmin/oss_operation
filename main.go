package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	// 自动创建 .env 文件（如果不存在的话）
	createEnvFileIfNotExist()
	// 加载 .env 文件中的环境变量
	err := godotenv.Load(".env")
	endpoint := os.Getenv("OSS_ENDPOINT")
	accessKeyID := os.Getenv("OSS_ACCESS_KEY_ID")
	accessKeySecret := os.Getenv("OSS_ACCESS_KEY_SECRET")
	bucketName := os.Getenv("OSS_BUCKET_NAME")
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	// region := "oss-cn-hangzhou"
	client, err := oss.New(endpoint, accessKeyID, accessKeySecret)
	if err != nil {
		log.Fatal("Failed to create OSS client: ", err)
	}
	// 获取 Bucket 对象
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		log.Fatal("Failed to get bucket: ", err)
	}

	// 创建一个默认的 Gin 路由引擎
	r := gin.Default()

	// 定义一个 GET 路由
	r.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "Hello, Gin!",
		})
	})

	// 定义一个带参数的 GET 路由
	r.GET("/isexist/:name", func(c *gin.Context) {
		name := c.Param("name") // 获取 URL 路径参数
		_, err := bucket.GetObjectMeta(name)
		if err != nil {
			if ossError, ok := err.(*oss.ServiceError); ok {
				// 如果是 404 错误，表示对象不存在
				if ossError.Code == "NoSuchKey" {
					c.JSON(http.StatusOK, gin.H{
						"message": fmt.Sprintf("Object '%s' does not exist", name),
					})
				} else {
					// 其他错误
					c.JSON(http.StatusInternalServerError, gin.H{
						"message": "Error checking object: " + ossError.Message,
					})
				}
			} else {
				// 如果出现非 OSS 错误
				c.JSON(http.StatusInternalServerError, gin.H{
					"message": "Error: " + err.Error(),
				})
			}
		} else {
			// 如果没有错误，表示对象存在
			c.JSON(http.StatusOK, gin.H{
				"message": fmt.Sprintf("Object '%s' exists", name),
			})
		}
	})

	// 路由处理文件下载
	r.GET("/download/:object", func(c *gin.Context) {
		objectName := c.Param("object") // 从URL参数获取对象名
		ext := filepath.Ext(objectName)
		if ext == "" {
			// 如果没有扩展名，可以选择给它一个默认的扩展名
			ext = ".bin"
		}
		// 获取文件元数据，查看文件大小
		meta, err := bucket.GetObjectMeta(objectName)
		if err != nil {
			log.Printf("Failed to get object metadata: %v", err)
			c.JSON(500, gin.H{
				"message": "Failed to get object metadata",
			})
			return
		}

		// 获取文件大小
		fileSize := meta["Content-Length"] // 从 metadata 中获取文件大小
		// 获取文件流
		body, err := bucket.GetObject(objectName)
		if err != nil {
			log.Fatalf("Failed to get object: %v", err)
			c.JSON(500, gin.H{
				"message": "Failed to get object",
			})
		}
		defer body.Close()
		filename := generateRandomFilename(ext)
		// 设置响应头
		c.Header("Content-Disposition", "attachment; filename="+filename)
		c.Header("Content-Type", mime.TypeByExtension(ext))     // 根据扩展名设置 MIME 类型
		c.Header("Content-Length", fmt.Sprintf("%d", fileSize)) // 设置文件大小

		// 流式传输文件内容返回给客户端
		_, err = io.Copy(c.Writer, body)
		if err != nil {
			log.Printf("Failed to send file to client: %v", err)
			c.JSON(500, gin.H{
				"message": "Failed to send file to client",
			})
			return
		}
		log.Println("File downloaded successfully:", filename)
		c.JSON(200, gin.H{
			"message": "File downloaded successfully",
			"file":    filename,
		})
	})

	r.POST("/upload", func(c *gin.Context) {
		// 获取上传的文件
		file, err := c.FormFile("file")
		if err != nil {
			log.Printf("Failed to get file from form: %v", err)
			c.JSON(400, gin.H{"message": "Failed to get file"})
			return
		}
		// 指定要上传到 OSS 的文件路径（可以使用文件名或自定义路径）
		objectName := file.Filename
		src, err := file.Open()
		if err != nil {
			log.Fatalf("Failed to open file: %v", err)
			c.JSON(400, gin.H{"message": "Failed to open file"})
			return
		}
		defer src.Close()
		// 指定待上传的网络流。
		// 从网络流中读取数据，并将其上传至 OSS。
		err = bucket.PutObject(objectName, src)
		if err != nil {
			log.Fatalf("Failed to fetch URL: %v", err)
			c.JSON(500, gin.H{"message": "Failed to upload file to OSS"})
			return
		}

		log.Println("File uploaded successfully.")
		c.JSON(200, gin.H{"message": "File uploaded successfully"})
	})
	// 定义一个 POST 路由
	r.DELETE("/delete/:object", func(c *gin.Context) {
		objectName := c.Param("object") // 从URL参数获取对象名
		// 调用 OSS DeleteObject 方法删除对象
		err := bucket.DeleteObject(objectName)
		if err != nil {
			// 如果发生错误，返回失败响应
			c.JSON(500, gin.H{
				"status":  "error",
				"message": fmt.Sprintf("Failed to delete object: %s", err.Error()),
			})
			return
		}

		// 如果删除成功，返回成功响应
		c.JSON(200, gin.H{
			"status":  "success",
			"message": fmt.Sprintf("Object '%s' deleted successfully", objectName),
		})
	})
	r.GET("/list", func(c *gin.Context) {
		// 假设你已经设置好了 OSS 客户端和存储桶
		var allObjects []string
		marker := ""
		for {
			lsRes, err := bucket.ListObjects(oss.Marker(marker))
			if err != nil {
				log.Fatalf("Failed to list objects: %v", err)
				c.JSON(500, gin.H{
					"status":  "error",
					"message": fmt.Sprintf("Failed to list objects: %s", err.Error()),
				})
			}

			// 打印列举结果。默认情况下，一次返回100条记录。
			for _, object := range lsRes.Objects {
				allObjects = append(allObjects, object.Key)
			}

			// 如果还有更多对象需要列举，则更新marker并继续循环。
			if lsRes.IsTruncated {
				marker = lsRes.NextMarker
			} else {
				break
			}
		}

		log.Println("All objects have been listed.")
		c.JSON(200, gin.H{
			"status":  "success",
			"message": "All objects have been listed",
			"objects": allObjects,
		})

	})
	r.GET("/invertcode/:audio", func(c *gin.Context) {
		audio := c.Param("audio")
		// 调用 OSS GetObject 方法获取对象
		object, err := bucket.GetObject(audio)
		if err != nil {
			log.Println("Error getting object:", err)
			c.JSON(500, gin.H{
				"status":  "error",
				"message": "Failed to get object",
			})
			return
		}
		// 转码操作 TODO!
		c.JSON(200, gin.H{
			"message": "invertcode success",
			"file":    object,
		})
		defer object.Close()
	})
	// 启动服务器，监听端口 8080
	r.Run(":8080")
}

func generateRandomFilename(ext string) string {
	// 设置随机数种子为当前时间戳
	rand.Seed(time.Now().UnixNano())

	// 生成一个随机字符串
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	length := 10 // 文件名长度
	var filename []byte
	for i := 0; i < length; i++ {
		filename = append(filename, charset[rand.Intn(len(charset))])
	}

	// 使用时间戳作为文件名的一部分
	timestamp := time.Now().Unix()

	// 组合随机字符串和时间戳来生成文件名
	return fmt.Sprintf("%d_%s%s", timestamp, string(filename), ext)
}

// 自动创建 .env 文件并设置默认值
func createEnvFileIfNotExist() {
	// 检查 .env 文件是否存在
	if _, err := os.Stat(".env"); os.IsNotExist(err) {
		// 如果文件不存在，创建并写入默认值
		envContent := `OSS_ENDPOINT=oss-cn-hangzhou.aliyuncs.com
OSS_ACCESS_KEY_ID=your-access-key-id
OSS_ACCESS_KEY_SECRET=your-access-key-secret
OSS_BUCKET_NAME=your-bucket-name`

		// 创建文件
		err := os.WriteFile(".env", []byte(envContent), 0644)
		if err != nil {
			log.Fatalf("Failed to create .env file: %v", err)
		}
		fmt.Println(".env file created with default values.")
	} else {
		fmt.Println(".env file already exists.")
	}
}
