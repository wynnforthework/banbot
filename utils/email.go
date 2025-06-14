package utils

import (
	"crypto/tls"
	"fmt"
	"github.com/banbox/banexg/log"
	"go.uber.org/zap"
	"net/smtp"
	"sync"
)

// EmailTask 表示一个邮件发送任务
type EmailTask struct {
	From    string
	To      string
	Subject string
	Body    string
}

var (
	emailQueue     chan EmailTask
	emailQueueOnce sync.Once
	// 默认队列大小
	defaultQueueSize = 1000
	mailSender       *MailSender
)

// StartEmailWorker 启动邮件发送worker
func StartEmailWorker(queueSize int) {
	emailQueueOnce.Do(func() {
		if queueSize <= 0 {
			queueSize = defaultQueueSize
		}
		emailQueue = make(chan EmailTask, queueSize)

		go func() {
			log.Info("Email worker started")
			for task := range emailQueue {
				err := mailSender.SendMail(task.From, task.To, task.Subject, task.Body)
				if err != nil {
					log.Error("Failed to send email from queue",
						zap.Error(err),
						zap.String("to", task.To),
						zap.String("subject", task.Subject))
				} else {
					log.Info("send email ok", zap.String("to", task.To),
						zap.String("subject", task.Subject))
				}
			}
		}()
	})
}

func SetMailSender(host string, port int, username, password string) {
	mailSender = NewMailSender(host, port, username, password)
}

// SendEmailFrom sends an email using the provided configuration
func SendEmailFrom(from, to, subject, body string) error {
	// 确保邮件工作线程已经启动
	if emailQueue == nil {
		if mailSender == nil {
			return nil
		}
		StartEmailWorker(defaultQueueSize)
	}

	// 向队列中添加邮件任务
	task := EmailTask{
		From:    from,
		To:      to,
		Subject: subject,
		Body:    body,
	}

	select {
	case emailQueue <- task:
		return nil
	default:
		log.Warn("Email queue is full, task dropped", zap.String("to", to), zap.String("subject", subject))
		return fmt.Errorf("email queue is full")
	}
}

type MailSender struct {
	host     string
	port     int
	username string
	password string
	client   *smtp.Client
	mu       sync.Mutex
}

func NewMailSender(host string, port int, username, password string) *MailSender {
	return &MailSender{
		host:     host,
		port:     port,
		username: username,
		password: password,
	}
}

func (ms *MailSender) getClient() (*smtp.Client, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// 如果已有客户端且连接正常，直接返回
	if ms.client != nil {
		if err := ms.client.Noop(); err == nil {
			return ms.client, nil
		}
		// 连接已断开，关闭旧客户端
		ms.client.Close()
	}

	// 建立新连接
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         ms.host,
	}

	addr := fmt.Sprintf("%s:%d", ms.host, ms.port)
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("TLS dial failed: %v", err)
	}

	client, err := smtp.NewClient(conn, ms.host)
	if err != nil {
		return nil, fmt.Errorf("SMTP client creation failed: %v", err)
	}

	// 认证
	auth := smtp.PlainAuth("", ms.username, ms.password, ms.host)
	if err = client.Auth(auth); err != nil {
		return nil, fmt.Errorf("SMTP auth failed: %v", err)
	}

	ms.client = client
	return client, nil
}

func (ms *MailSender) SendMail(from string, to string, subject, body string) error {
	client, err := ms.getClient()
	if err != nil {
		return err
	}
	if from == "" {
		from = ms.username
	}

	message := []byte(
		"From: " + from + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"\r\n" +
			body + "\r\n")

	// 使用同一个客户端发送多封邮件
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if err = client.Mail(from); err != nil {
		return fmt.Errorf("MAIL command failed: %v", err)
	}
	if err = client.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT command failed: %v", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA command failed: %v", err)
	}
	if _, err = w.Write(message); err != nil {
		return fmt.Errorf("message writing failed: %v", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("message closing failed: %v", err)
	}

	return nil
}
