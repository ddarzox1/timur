// Package sms — отправка SMS.
package sms

import "log/slog"

// Log пишет SMS в лог, пока не подключён провайдер (Mobizon / SMSC.kz).
type Log struct{}

func (Log) Send(phone, text string) error {
	slog.Info("SMS", "phone", "+"+phone, "text", text)
	return nil
}
