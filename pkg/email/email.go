package email

import (
	"crypto/tls"
	"net/smtp"
)

type EmailClient struct {
	smtpHost           string
	smtpPort           string
	from               string
	fromDisplayName    string
	replyTo            string
	replyToDisplayName string
	username           string
	password           string
}

func NewEmailClient(smtpHost, smtpPort, from, fromDisplayName, replyTo, replyToDisplayName, username, password string) (ec *EmailClient) {
	return &EmailClient{
		smtpHost:           smtpHost,
		smtpPort:           smtpPort,
		from:               from,
		fromDisplayName:    fromDisplayName,
		replyTo:            replyTo,
		replyToDisplayName: replyToDisplayName,
		username:           username,
		password:           password,
	}
}

func (ec *EmailClient) SendEmail(to, subject, body string) error {
	var replyTo string
	if ec.replyTo != "" {
		if ec.replyToDisplayName != "" {
			replyTo = "Reply-To: " + ec.replyToDisplayName + " <" + ec.replyTo + ">\r\n"
		} else {
			replyTo = "Reply-To: " + ec.replyTo + "\r\n"
		}
	}

	message := []byte("From: " + ec.fromDisplayName + " <" + ec.from + ">\r\n" +
		replyTo +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"\r\n" +
		body)

	// Authentication credentials
	auth := smtp.PlainAuth("", ec.username, ec.password, ec.smtpHost)

	client, err := smtp.Dial(ec.smtpHost + ":" + ec.smtpPort)
	if err != nil {
		return err
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         ec.smtpHost,
	}

	if err = client.StartTLS(tlsConfig); err != nil {
		return err
	}

	// Authenticate and send the email
	if err = client.Auth(auth); err != nil {
		return err
	}

	if err = client.Mail(ec.from); err != nil {
		return err
	}

	if err = client.Rcpt(to); err != nil {
		return err
	}

	w, err := client.Data()
	if err != nil {
		return err
	}

	_, err = w.Write(message)
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	err = client.Quit()
	if err != nil {
		return err
	}

	return nil
}
