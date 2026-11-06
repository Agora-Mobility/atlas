package emails

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strings"

	"github.com/Agora-Mobility/atlas/domain/services"
)

//go:embed templates/message.html
var files embed.FS

var page = template.Must(template.ParseFS(files, "templates/message.html"))

type Message struct {
	Subject     string
	Greeting    string
	Lines       []string
	ActionLabel string
	ActionURL   string
	Notes       []string
}

func (m Message) Render(to, idempotencyKey string) (services.Email, error) {
	var html bytes.Buffer
	if err := page.Execute(&html, m); err != nil {
		return services.Email{}, fmt.Errorf("render email %q: %w", m.Subject, err)
	}
	return services.Email{
		To:             to,
		Subject:        m.Subject,
		HTML:           html.String(),
		Text:           m.text(),
		IdempotencyKey: idempotencyKey,
	}, nil
}

func (m Message) text() string {
	var b strings.Builder
	b.WriteString(m.Greeting + "\n\n")
	for _, line := range m.Lines {
		b.WriteString(line + "\n\n")
	}
	if m.ActionURL != "" {
		b.WriteString(m.ActionLabel + ": " + m.ActionURL + "\n\n")
	}
	for _, note := range m.Notes {
		b.WriteString(note + "\n")
	}
	b.WriteString("\nEquipa AGORA. Esta mensagem foi enviada automaticamente, não responda.\n")
	return b.String()
}

func Greeting(fullName string) string {
	name, _, _ := strings.Cut(strings.TrimSpace(fullName), " ")
	if name == "" {
		return "Olá,"
	}
	return "Olá " + name + ","
}

func Invitation(name, organization, link string) Message {
	return Message{
		Subject:     "Convite para " + organization + " na AGORA",
		Greeting:    Greeting(name),
		Lines:       []string{"Foi convidado(a) para fazer parte de " + organization + " na AGORA.", "Para activar a conta, abra o convite, confirme o código que vai receber por SMS e defina a sua palavra-passe."},
		ActionLabel: "Activar a conta",
		ActionURL:   link,
		Notes:       []string{"O convite é válido durante 48 horas e só pode ser usado uma vez.", "Se não esperava este convite, ignore esta mensagem."},
	}
}

func MemberActivated(name, organization string) Message {
	return Message{
		Subject:  "A sua conta em " + organization + " está activa",
		Greeting: Greeting(name),
		Lines:    []string{"A sua conta em " + organization + " foi activada. Já pode entrar com o seu e-mail ou telefone e a palavra-passe que definiu."},
		Notes:    []string{"Se não foi você a activar esta conta, contacte de imediato o administrador da sua organização."},
	}
}

func MemberRolesChanged(name, organization string) Message {
	return Message{
		Subject:  "Os seus acessos em " + organization + " foram alterados",
		Greeting: Greeting(name),
		Lines:    []string{"Os papéis e permissões da sua conta em " + organization + " foram alterados por um administrador.", "As alterações já estão em vigor."},
		Notes:    []string{"Se não reconhece esta alteração, contacte o administrador da sua organização."},
	}
}

func MemberSuspended(name, organization string) Message {
	return Message{
		Subject:  "O seu acesso a " + organization + " foi suspenso",
		Greeting: Greeting(name),
		Lines:    []string{"O seu acesso a " + organization + " foi suspenso por um administrador e as sessões abertas foram terminadas.", "Para saber o motivo, contacte o administrador da sua organização."},
	}
}

func MemberReactivated(name, organization string) Message {
	return Message{
		Subject:  "O seu acesso a " + organization + " foi reactivado",
		Greeting: Greeting(name),
		Lines:    []string{"O seu acesso a " + organization + " foi reactivado. Já pode voltar a entrar."},
	}
}

func MemberDeactivated(name, organization string) Message {
	return Message{
		Subject:  "O seu acesso a " + organization + " foi desactivado",
		Greeting: Greeting(name),
		Lines:    []string{"O seu acesso a " + organization + " foi desactivado de forma definitiva e as sessões abertas foram terminadas."},
		Notes:    []string{"Se acha que se trata de um engano, contacte o administrador da sua organização."},
	}
}

func PasswordRecoveryCode(name, code string, validMinutes int) Message {
	return Message{
		Subject:  "Código para recuperar a palavra-passe",
		Greeting: Greeting(name),
		Lines:    []string{"Use este código para criar uma nova palavra-passe na AGORA:", code},
		Notes: []string{
			fmt.Sprintf("O código é válido durante %d min e só pode ser usado uma vez. Não o partilhe.", validMinutes),
			"Se não foi você a pedir este código, ignore esta mensagem. A sua palavra-passe não muda.",
		},
	}
}

func AccountBlocked(name string) Message {
	return Message{
		Subject:  "A sua conta AGORA foi bloqueada",
		Greeting: Greeting(name),
		Lines:    []string{"A sua conta AGORA foi bloqueada e as sessões abertas foram terminadas.", "Para saber o motivo ou pedir a revisão, contacte o apoio ao cliente."},
	}
}

func AccountUnblocked(name string) Message {
	return Message{
		Subject:  "A sua conta AGORA foi desbloqueada",
		Greeting: Greeting(name),
		Lines:    []string{"A sua conta AGORA foi desbloqueada. Já pode voltar a entrar na aplicação."},
	}
}
