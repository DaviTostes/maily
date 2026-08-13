package ui

import "testing"

// Cases are real subject/snippet pairs from an inbox, trimmed to snippet length.
func TestExtractCode(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		snippet string
		want    string
	}{
		{
			"scalefy pt-BR",
			"Código de verificação - Scalefy",
			"SCALEFY Pagamentos Verificação de acesso Olá, Davi Tostes! Use o código abaixo para concluir o seu login na plataforma Scalefy. Seu código 121435 ⏱ Este código é válido por 15 minutos e pode ser usado",
			"121435",
		},
		{
			"leading zero survives",
			"Código de verificação - Scalefy",
			"SCALEFY Pagamentos Verificação de acesso Olá! Seu código 007012 ⏱ Este código é válido por 15 minutos",
			"007012",
		},
		{
			"magalu otp",
			"ID Magalu - Código de confirmação",
			"OTP Email Template ID Magalu OTP Email Template Internal Content Olá, Este é seu código de verificação: 958424 Esse código nunca será solicitado",
			"958424",
		},
		{
			"microsoft one-time",
			"Seu código de uso único",
			"Olá, Nós recebemos uma solicitação para um código de uso único para a sua conta da Microsoft. Seu código de uso único é: 598114 Insira este código apenas",
			"598114",
		},
		{
			"code before the keyword",
			"483920 is your login code",
			"Enter it to verify your account.",
			"483920",
		},
		{
			"alphanumeric code",
			"Your verification code",
			"Your one-time code is A1B2C3 and expires shortly.",
			"A1B2C3",
		},
		// Negatives — a wrong copy silently clobbers the clipboard.
		{
			"steam receipt has no code",
			"You have sold an item on the Community Market",
			"Dear mrmine31, An item you listed in the Community Market has been sold. Your Steam Wallet has been credited 0.60 BRL. Confirmation Number 529887366699587567-529887366699587568 Date Confirmed Wed Aug 12 13:58:30 2026",
			"",
		},
		{
			"pix transfer",
			"Você recebeu uma transferência pelo Pix",
			"Boa notícia! Você recebeu R$ 150,00 de Fulano em 12 de agosto de 2026.",
			"",
		},
		{
			"keyword but only a year",
			"Security notice",
			"Your security settings changed in 2026. No code was requested.",
			"",
		},
		{
			"no keyword, plenty of numbers",
			"Sua nota fiscal : )",
			"Pedido 12345678 enviado em 2026, valor 4321.",
			"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractCode(tc.subject, tc.snippet); got != tc.want {
				t.Errorf("extractCode() = %q, want %q", got, tc.want)
			}
		})
	}
}
