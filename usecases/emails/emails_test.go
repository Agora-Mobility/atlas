package emails_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/usecases/emails"
)

func TestInvitationRendersHTMLAndText(t *testing.T) {
	email, err := emails.Invitation("Ana Domingos", "Transportes Kianda", "https://painel.agora.ao/convite?token=abc").Render("ana@agora.ao", "invitation/1")

	require.NoError(t, err)
	assert.Equal(t, "ana@agora.ao", email.To)
	assert.Equal(t, "Convite para Transportes Kianda na AGORA", email.Subject)
	assert.Equal(t, "invitation/1", email.IdempotencyKey)
	assert.Contains(t, email.HTML, "Olá Ana,")
	assert.Contains(t, email.HTML, `href="https://painel.agora.ao/convite?token=abc"`)
	assert.Contains(t, email.Text, "Activar a conta: https://painel.agora.ao/convite?token=abc")
}

func TestUserInputIsEscapedInHTML(t *testing.T) {
	email, err := emails.MemberSuspended("<script>x</script>", `Kianda <b>"Lda"</b>`).Render("ana@agora.ao", "k")

	require.NoError(t, err)
	assert.NotContains(t, email.HTML, "<script>")
	assert.NotContains(t, email.HTML, "<b>")
	assert.Contains(t, email.HTML, "&lt;b&gt;")
}

func TestGreetingWithoutName(t *testing.T) {
	assert.Equal(t, "Olá,", emails.Greeting("  "))
	assert.Equal(t, "Olá Ana,", emails.Greeting("Ana Maria Domingos"))
}
