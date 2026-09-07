package services

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strconv"
	"strings"
)

type EmailService struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	Enabled  bool
}

var Email *EmailService

func InitEmailService() {
	host := os.Getenv("SMTP_HOST")
	portStr := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	password := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")

	if host == "" {
		host = "smtp.gmail.com"
	}

	port := 587
	if portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}

	if from == "" {
		from = user
	}
	if from == "" {
		from = "no-reply@lemas.io.vn"
	}

	enabled := user != "" && password != ""

	Email = &EmailService{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		From:     from,
		Enabled:  enabled,
	}

	if enabled {
		log.Printf("[Email Service] ✅ SMTP configured with host: %s:%d, sender: %s", host, port, from)
	} else {
		log.Printf("[Email Service] ℹ️ SMTP credentials not set. OTP codes will be printed to server logs for easy local testing.")
	}
}

func (s *EmailService) SendVerificationOTP(toEmail, userName, otpCode string) error {
	subject := fmt.Sprintf("Mã xác thực đăng ký Lemas.AI: %s", otpCode)
	if userName == "" {
		userName = "bạn"
	}

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #0b0f19; color: #f1f5f9; padding: 20px; }
  .card { max-width: 520px; margin: 0 auto; background: #131b2e; border: 1px solid rgba(255,255,255,0.1); border-radius: 16px; padding: 32px; box-shadow: 0 10px 30px rgba(0,0,0,0.5); }
  .logo { font-size: 24px; font-weight: 800; color: #10b981; margin-bottom: 20px; text-align: center; }
  .otp-box { background: rgba(16, 185, 129, 0.1); border: 2px dashed #10b981; border-radius: 12px; padding: 18px; font-size: 32px; font-weight: 900; letter-spacing: 8px; text-align: center; color: #34d399; margin: 24px 0; }
  .footer { font-size: 12px; color: #64748b; text-align: center; margin-top: 24px; line-height: 1.5; }
</style>
</head>
<body>
<div class="card">
  <div class="logo">Lemas.AI Gateway</div>
  <h2 style="color: #ffffff; font-size: 20px; margin-top: 0;">Chào %s,</h2>
  <p style="color: #94a3b8; font-size: 14px; line-height: 1.6;">
    Cảm ơn bạn đã đăng ký tài khoản tại <strong>Lemas.AI</strong>. Vui lòng sử dụng mã xác thực bên dưới để hoàn tất xác minh email của bạn:
  </p>
  <div class="otp-box">%s</div>
  <p style="color: #94a3b8; font-size: 13px; line-height: 1.5;">
    ⏱️ Mã này có hiệu lực trong vòng <strong>10 phút</strong>. Vì lý do bảo mật, tuyệt đối không chia sẻ mã này cho bất kỳ ai.
  </p>
  <div class="footer">
    Nếu bạn không thực hiện yêu cầu này, vui lòng bỏ qua email.<br>
    © 2026 Lemas.AI - Nền tảng AI Gateway & Model Hub
  </div>
</div>
</body>
</html>`, userName, otpCode)

	// If SMTP is not configured, print to server console for testing
	log.Printf("==================================================================")
	log.Printf("📧 [VERIFICATION OTP EMAIL] To: %s | Name: %s | OTP Code: %s", toEmail, userName, otpCode)
	log.Printf("==================================================================")

	if !s.Enabled {
		return nil
	}

	// Send via SMTP
	headers := make(map[string]string)
	headers["From"] = fmt.Sprintf("Lemas.AI <%s>", s.From)
	headers["To"] = toEmail
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/html; charset=\"UTF-8\""

	message := ""
	for k, v := range headers {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + htmlBody

	auth := smtp.PlainAuth("", s.User, s.Password, s.Host)
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)

	// TLS config
	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         s.Host,
	}

	if s.Port == 465 {
		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			log.Printf("[Email Error] SSL Dial failed: %v", err)
			return err
		}
		defer conn.Close()

		c, err := smtp.NewClient(conn, s.Host)
		if err != nil {
			log.Printf("[Email Error] SMTP client creation failed: %v", err)
			return err
		}
		defer c.Quit()

		if err = c.Auth(auth); err != nil {
			log.Printf("[Email Error] SMTP auth failed: %v", err)
			return err
		}

		if err = c.Mail(s.From); err != nil {
			return err
		}
		if err = c.Rcpt(toEmail); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(message))
		if err != nil {
			return err
		}
		return w.Close()
	}

	// Standard STARTTLS (Port 587)
	err := smtp.SendMail(addr, auth, s.From, []string{toEmail}, []byte(message))
	if err != nil {
		log.Printf("[Email Error] SendMail failed: %v", err)
		return err
	}

	log.Printf("[Email Service] ✅ Verification email successfully sent to %s", toEmail)
	return nil
}

func CleanEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}
