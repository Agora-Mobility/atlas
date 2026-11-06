# RF21 – Gestão de utilizadores (RBAC multi-âmbito)

| Campo | Valor |
|---|---|
| Requisito | RF21 – Gestão de utilizadores (prioridade Alta) |
| Ciclo previsto no plano MVP 1 | C7 (semanas 13–14) |
| Requisitos relacionados | RF26 (auditoria operacional), RF22 (gestão de prestadores), RF02 (autenticação) |
| ADR relacionados | 0006 (autenticação OTP + JWT), 0013 (utilizadores do backoffice e auditoria), 0014 (schema `atlas`) |
| Estado | Rascunho – aguarda aprovação |
| Data | 2026-10-29 |

## 1. Objectivo

Dar à AGORA e aos parceiros a gestão dos seus utilizadores internos, com um
**único motor de autorização por permissões e âmbitos (RBAC multi-âmbito)**:

- o âmbito **PLATFORM** serve a equipa AGORA no backoffice **argus**;
- o âmbito **PARTNER** serve as empresas parceiras no backoffice **athena**,
  cada uma isolada das restantes (multi-tenant).

Os mesmos endpoints servem os dois âmbitos. A organização e o âmbito do actor
vêm sempre do token, nunca do pedido.

Clientes (**iris**) e entregadores/prestadores (**hermes**) **não** entram neste
RBAC. O acesso deles é por posse ("o recurso é teu?").

## 2. Glossário

| Termo | Significado |
|---|---|
| Utilizador | Identidade de uma pessoa: e-mail, telefone, palavra-passe e estado. Não tem papéis. |
| Organização | Entidade a que um utilizador pertence. Tipo `PLATFORM` (uma única, a AGORA) ou `PARTNER`. |
| Membership | Pertença de um utilizador a uma organização. Tem estado, papéis e `perm_version`. |
| Âmbito (scope) | `PLATFORM` ou `PARTNER`. É o tipo da organização da membership. |
| Permissão | Capacidade atómica `recurso:acção`, de um catálogo fixo em código. Declara os âmbitos onde é permitida. |
| Papel | Conjunto nomeado de permissões de um só âmbito. Pode ser de sistema (template) ou da organização. |
| Permissões efectivas | União das permissões de todos os papéis da membership activa. |
| `perm_version` | Contador da membership, incrementado sempre que as suas permissões ou o seu estado mudam. |
| Actor | Membership autenticada que executa a acção. |
| Alvo | Membership sobre a qual a acção incide. |

## 3. Actores

| Actor | Cliente | Âmbito | Descrição |
|---|---|---|---|
| Super Admin AGORA | argus | PLATFORM | Todas as permissões PLATFORM. |
| Membro AGORA | argus | PLATFORM | Administrador, Operações, Financeiro, Suporte ou Auditor. |
| Proprietário do parceiro | athena | PARTNER | Todas as permissões PARTNER e de membros. |
| Membro do parceiro | athena | PARTNER | Gestor, Operador ou Financeiro. |
| Convidado | argus / athena | – | Pessoa com um convite pendente. Ainda não autentica. |
| Sistema | – | – | Seeds, arranque do primeiro Super Admin e tarefas internas. |

Fora do RBAC: clientes (iris) e entregadores/prestadores (hermes).

## 4. Catálogo de permissões

| Permissão | PLATFORM | PARTNER | Descrição |
|---|:-:|:-:|---|
| `members:read` | ✓ | ✓ | Ver membros da própria organização |
| `members:invite` | ✓ | ✓ | Convidar membros e reenviar convites |
| `members:update` | ✓ | ✓ | Alterar nome e telefone de um membro |
| `members:suspend` | ✓ | ✓ | Suspender e reactivar membros |
| `members:deactivate` | ✓ | ✓ | Desactivar membros (definitivo) |
| `roles:read` | ✓ | ✓ | Ver papéis e o catálogo de permissões |
| `roles:assign` | ✓ | ✓ | Atribuir papéis a membros |
| `roles:manage` | ✓ | | Gerir papéis (sem endpoints no MVP 1) |
| `partners:read` | ✓ | | Ver parceiros e os seus membros |
| `partners:manage` | ✓ | | Gerir parceiros e convidar os seus membros |
| `providers:read` | ✓ | | Ver prestadores |
| `providers:approve` | ✓ | | Aprovar e rejeitar prestadores |
| `providers:suspend` | ✓ | | Suspender prestadores |
| `categories:manage` | ✓ | | Gerir categorias |
| `services:manage` | ✓ | | Gerir serviços |
| `requests:read` | ✓ | | Ver pedidos de serviço |
| `requests:manage` | ✓ | | Gerir pedidos de serviço |
| `requests:cancel` | ✓ | | Cancelar pedidos de serviço |
| `finance:read` | ✓ | | Ver dados financeiros |
| `refunds:create` | ✓ | | Criar reembolsos |
| `refunds:approve` | ✓ | | Aprovar reembolsos |
| `reconciliation:run` | ✓ | | Correr a reconciliação |
| `audit:read` | ✓ | | Ler o registo de auditoria |
| `partner.providers:read` | | ✓ | Ver os prestadores do parceiro |
| `partner.providers:manage` | | ✓ | Gerir os prestadores do parceiro |
| `partner.requests:read` | | ✓ | Ver os pedidos do parceiro |
| `partner.finance:read` | | ✓ | Ver as finanças do parceiro |

As permissões `members:*`, `roles:read` e `roles:assign` actuam sempre **dentro
da organização do actor**, em qualquer âmbito.

## 5. Papéis de sistema

| Âmbito | Papel | Permissões |
|---|---|---|
| PLATFORM | Super Admin | Todas as permissões PLATFORM |
| PLATFORM | Administrador | Todas as PLATFORM, excepto `roles:manage` e `refunds:approve` |
| PLATFORM | Operações | `providers:*`, `categories:manage`, `services:manage`, `requests:*`, `members:read` |
| PLATFORM | Financeiro | `finance:read`, `refunds:create`, `refunds:approve`, `reconciliation:run` |
| PLATFORM | Suporte | `members:read`, `providers:read`, `requests:read`, `requests:cancel` |
| PLATFORM | Auditor | Todas as `*:read` PLATFORM e `audit:read` |
| PARTNER | Proprietário | Todas as PARTNER e todas as de membros (`members:*`, `roles:read`, `roles:assign`) |
| PARTNER | Gestor | `members:read`, `members:invite`, `members:update`, `members:suspend`, `roles:read`, `roles:assign`, `partner.providers:*`, `partner.requests:read` |
| PARTNER | Operador | `partner.providers:read`, `partner.providers:manage`, `partner.requests:read` |
| PARTNER | Financeiro | `partner.finance:read`, `partner.requests:read` |

No MVP 1 os papéis de sistema não são editáveis por API. O modelo suporta
papéis personalizados por organização, mas sem endpoints para os criar.

## 6. Ciclo de vida da membership

```
            convite                aceitar convite
  (nada) ───────────▶ INVITED ───────────────────▶ ACTIVE ◀──────────┐
                         │                          │  │             │ reactivar
                         │ desactivar               │  └─ suspender ─▶ SUSPENDED
                         ▼                          │                  │
                    DEACTIVATED ◀──── desactivar ───┴──────────────────┘
```

- Só uma membership `ACTIVE` autentica.
- `DEACTIVATED` é um estado final. A membership não volta a ficar activa
  (ver Q8).
- Não há eliminação física de utilizadores nem de memberships.

## 7. Fluxo principal

### 7.1 Convite
1. O actor com `members:invite` envia o e-mail, o nome, o telefone e os
   papéis (`role_ids`) do convidado.
2. O sistema valida as regras RN01, RN02 e RN13.
3. O sistema cria, se ainda não existirem, o utilizador (sem palavra-passe) e
   a membership `INVITED` na organização do actor, com os papéis pedidos.
4. O sistema gera um token de convite aleatório e guarda só o hash
   (SHA-256), com validade de 48 h.
5. O sistema envia o convite ao convidado (canal: Q2). O token em claro só
   aparece nessa mensagem.
6. O sistema regista a acção em `audit_logs` (RN11).

### 7.2 Activação (aceitar convite)
1. O convidado abre a ligação e envia o token e a nova palavra-passe.
2. O sistema valida o token: existe, não expirou, não foi usado nem
   substituído.
3. O sistema envia um OTP (canal: Q2) e o convidado submete-o.
4. O sistema guarda o hash da palavra-passe, marca o convite como aceite e
   passa a membership a `ACTIVE`.
5. O sistema regista a acção em `audit_logs`.

### 7.3 Gestão de papéis
1. O actor com `roles:assign` envia a lista completa de papéis do alvo
   (`PUT`, que substitui a lista).
2. O sistema valida RN01, RN02, RN03 e RN04.
3. O sistema substitui os papéis, incrementa `perm_version` e invalida a
   cache de permissões do alvo (RN07).
4. O sistema regista a acção com os papéis antes e depois.

### 7.4 Suspensão e reactivação
1. O actor com `members:suspend` suspende um membro `ACTIVE`.
2. O sistema valida RN03 e RN04.
3. O sistema passa a membership a `SUSPENDED`, revoga de imediato as sessões
   e os refresh tokens, e incrementa `perm_version` (RN06).
4. Reactivar passa uma membership `SUSPENDED` a `ACTIVE` e incrementa
   `perm_version`.
5. Ambas as acções ficam auditadas.

### 7.5 Desactivação
1. O actor com `members:deactivate` desactiva um membro `INVITED`, `ACTIVE`
   ou `SUSPENDED`.
2. O sistema valida RN03 e RN04.
3. O sistema passa a membership a `DEACTIVATED`, revoga sessões e convites
   pendentes, e incrementa `perm_version`.
4. A acção fica auditada.

### 7.6 Gestão de membros de um parceiro pela AGORA
1. Um actor PLATFORM com `partners:read` lista os membros de um parceiro.
2. Um actor PLATFORM com `partners:manage` convida membros para um parceiro,
   por exemplo o primeiro Proprietário. Os papéis têm de ser de âmbito
   PARTNER.
3. É a única forma de um actor actuar noutra organização. Fica auditada com
   a organização do alvo.

## 8. Endpoints

O prefixo segue a decisão de Q5 (`/v1` pedido ou `/api/v1` em uso).

| Método | Caminho | Permissão | Descrição |
|---|---|---|---|
| GET | `/v1/me` | autenticado | Perfil e organização activa |
| GET | `/v1/me/permissions` | autenticado | Permissões efectivas (só para a UI) |
| GET | `/v1/members` | `members:read` | Lista paginada; filtros `status`, `role`, `search` |
| POST | `/v1/members/invitations` | `members:invite` | Convidar (e-mail, nome, telefone, `role_ids`) |
| GET | `/v1/members/{id}` | `members:read` | Um membro |
| PATCH | `/v1/members/{id}` | `members:update` | Alterar nome e telefone |
| PUT | `/v1/members/{id}/roles` | `roles:assign` | Substituir os papéis |
| POST | `/v1/members/{id}/suspend` | `members:suspend` | Suspender |
| POST | `/v1/members/{id}/reactivate` | `members:suspend` | Reactivar |
| POST | `/v1/members/{id}/deactivate` | `members:deactivate` | Desactivar |
| POST | `/v1/members/{id}/invitations/resend` | `members:invite` | Reenviar o convite |
| GET | `/v1/roles` | `roles:read` | Papéis do âmbito da organização do actor |
| GET | `/v1/permissions` | `roles:read` | Catálogo de permissões do âmbito do actor |
| POST | `/v1/invitations/accept` | público | Aceitar o convite (token, palavra-passe, OTP) |
| GET | `/v1/partners/{orgId}/members` | `partners:read` (PLATFORM) | Membros de um parceiro |
| POST | `/v1/partners/{orgId}/members/invitations` | `partners:manage` (PLATFORM) | Convidar membros de um parceiro |

Os endpoints `/v1/members*` nunca aceitam `organization_id` no corpo nem na
query: a organização é a do actor, tirada do token.

## 9. Regras de negócio

| Id | Regra |
|---|---|
| RN01 | **Sem escalada de privilégios.** O actor só atribui (no convite ou em `PUT /roles`) papéis cujas permissões estejam todas contidas nas suas permissões efectivas. |
| RN02 | Um papel só se atribui a uma membership da mesma organização (ou papel de sistema) e do mesmo âmbito. |
| RN03 | Ninguém altera os próprios papéis, nem se suspende ou desactiva a si mesmo. |
| RN04 | Não é possível retirar o papel, suspender ou desactivar o último Super Admin `ACTIVE` da AGORA, nem o último Proprietário `ACTIVE` de um parceiro. |
| RN05 | Papéis de sistema não podem ser apagados nem alterados por API. |
| RN06 | Suspender ou desactivar revoga de imediato todas as sessões e refresh tokens da membership e incrementa `perm_version`. |
| RN07 | Alterar papéis incrementa `perm_version` e invalida a cache de permissões no Redis. O efeito é imediato no pedido seguinte. |
| RN08 | O convite expira em 48 h, é de uso único e o reenvio invalida o anterior. Aceitar exige definir a palavra-passe e validar o OTP. |
| RN09 | ~~MFA obrigatório para memberships com Super Admin, Financeiro (PLATFORM) ou Proprietário (PARTNER).~~ **Removida** (ADR 0019, migration `000036`): todas as contas entram com telefone e palavra-passe. |
| RN10 | Uma membership em estado diferente de `ACTIVE` não autentica. |
| RN11 | Cada acção deste módulo gera um registo em `audit_logs`, com estado antes e depois, na mesma transacção da alteração. |
| RN12 | As mensagens de erro e as respostas de login e de convite não revelam se um telefone ou e-mail existe. |
| RN13 | Um papel só contém permissões permitidas no seu âmbito. É validado no serviço e por constraint/trigger na base de dados. |
| RN14 | **Isolamento de tenant.** Um recurso de outra organização responde 404, nunca 403. Só um actor PLATFORM com permissão explícita (`partners:*`) atravessa organizações. |
| RN15 | Os endpoints verificam permissões, nunca nomes de papéis. |
| RN16 | Dados de parceiro estão protegidos em duas camadas: toda a query recebe `organization_id`, e a base de dados aplica Row-Level Security com o contexto do pedido. Uma query sem filtro não devolve dados de outro parceiro. |
| RN17 | Uma acção sobre um membro `DEACTIVATED` (excepto ler) é rejeitada com conflito. |

## 10. Fluxos alternativos e erros

| Situação | Resposta |
|---|---|
| Sem token, token inválido, expirado ou de sessão revogada | 401 |
| Token de outro cliente (ex.: token athena usado no argus) | 401 ou 403, conforme o padrão actual (`TOKEN_CLIENT_MISMATCH`) |
| Actor sem a permissão do endpoint | 403 `PERMISSION_DENIED` |
| Membro, papel ou organização de outro tenant | 404 (RN14) |
| Membro inexistente | 404 |
| Corpo inválido, campos em falta, formato errado | 400 ou 422, conforme o padrão actual da API |
| Papel de outro âmbito ou de outra organização | 422 `ROLE_NOT_ASSIGNABLE` (RN02) |
| Papel com permissões que o actor não tem | 403 `PRIVILEGE_ESCALATION` (RN01) |
| Acção sobre si próprio | 403 `CANNOT_MANAGE_SELF` (RN03) |
| Último Super Admin ou Proprietário | 409 `LAST_OWNER` (RN04) |
| Transição de estado inválida (ex.: reactivar quem está `ACTIVE`, agir sobre `DEACTIVATED`) | 409 `INVALID_MEMBER_STATE` |
| E-mail já é membro da organização | 409 `ALREADY_MEMBER`, só dentro da organização do actor e sem revelar contas de outras organizações (RN12) |
| Convite expirado, usado, substituído ou inexistente | 422 `INVALID_INVITATION`, a mesma resposta para todos os casos (RN12) |
| OTP errado, expirado ou com tentativas esgotadas | 422, com os códigos actuais (`INVALID_CODE`, `CODE_EXPIRED`, `CODE_ATTEMPTS_EXCEEDED`) |
| Login de membership não `ACTIVE` | Mesma resposta genérica de credenciais inválidas ou conta inactiva (RN10, RN12) |
| Redis indisponível ao resolver permissões | Fallback para a base de dados. Se a revogação de sessões não puder ser verificada, 503 (fail-closed, como em ADR 0006) |

## 11. Requisitos não funcionais aplicáveis

O plano MVP 1 refere RNF01–RNF20 mas não os detalha. A numeração exacta
fica para confirmar (Q10).

| Tema | Exigência para o RF21 |
|---|---|
| Segurança | Senhas em argon2id; tokens de convite e OTP só em hash; fail-closed na revogação. |
| Isolamento | RLS activa nas tabelas com `organization_id`, incluindo para o dono das tabelas. |
| Auditoria | `audit_logs` só aceita inserções, na mesma transacção da alteração (ADR 0013). |
| Desempenho | Verificar uma permissão com cache quente não acrescenta consultas à base de dados; p95 a definir em Q10. |
| Privacidade | Sem dados pessoais reais em seeds ou fixtures. Palavras-passe, tokens e OTP nunca aparecem nos logs. |
| Observabilidade | Recusas por permissão e por tenant registadas nos logs técnicos com correlation id, sem dados pessoais. |
| Disponibilidade | Falha do Redis não impede decisões de autorização (fallback para a base de dados). |

## 12. Critérios de aceitação

### RN01 – Sem escalada de privilégios
- **CA01.1** Dado um Gestor (PARTNER) sem `partner.finance:read`, quando atribui o papel Financeiro (PARTNER) a um membro, então recebe 403 `PRIVILEGE_ESCALATION` e os papéis do alvo não mudam.
- **CA01.2** Dado um Gestor, quando convida alguém com o papel Operador, então o convite é criado, porque as permissões do Operador estão contidas nas do Gestor.
- **CA01.3** Dado um Administrador (PLATFORM), quando atribui Super Admin, então recebe 403 `PRIVILEGE_ESCALATION`, porque não tem `roles:manage` nem `refunds:approve`.

### RN02 – Papel da mesma organização e âmbito
- **CA02.1** Dado um Proprietário do parceiro A, quando atribui um papel PLATFORM a um membro, então recebe 422 `ROLE_NOT_ASSIGNABLE`.
- **CA02.2** Dado um papel personalizado da organização B, quando um actor da organização A o atribui, então recebe 404 e o papel não é revelado.

### RN03 – Nada sobre si próprio
- **CA03.1** Dado um Proprietário, quando altera os próprios papéis, então recebe 403 `CANNOT_MANAGE_SELF`.
- **CA03.2** Dado um Super Admin, quando se suspende ou desactiva a si mesmo, então recebe 403 `CANNOT_MANAGE_SELF`.

### RN04 – Último Super Admin ou Proprietário
- **CA04.1** Dada a AGORA com um único Super Admin `ACTIVE`, quando outro actor tenta suspendê-lo, desactivá-lo ou retirar-lhe o papel, então recebe 409 `LAST_OWNER`.
- **CA04.2** Dado um parceiro com dois Proprietários `ACTIVE`, quando um suspende o outro, então a operação é aceite; e quando se tenta a seguir retirar o papel ao Proprietário restante, então recebe 409 `LAST_OWNER`.

### RN05 – Papéis de sistema imutáveis
- **CA05.1** Dado um papel de sistema, quando alguém tenta alterá-lo ou apagá-lo por API ou directamente na base de dados com o utilizador da aplicação, então a operação é recusada.

### RN06 – Revogação imediata
- **CA06.1** Dado um membro com sessão activa, quando é suspenso, então o pedido seguinte com o mesmo access token recebe 401 e o refresh token deixa de funcionar.
- **CA06.2** Dado um membro suspenso, quando é desactivado, então `perm_version` aumenta e o login devolve a resposta genérica.

### RN07 – Papéis com efeito imediato
- **CA07.1** Dado um Operador com permissões em cache, quando lhe retiram `partner.providers:manage`, então o pedido seguinte a um endpoint que exige essa permissão recebe 403, sem esperar pela expiração do token.
- **CA07.2** Dado um membro, quando os papéis mudam, então `perm_version` aumenta exactamente uma vez e a entrada antiga da cache deixa de ser usada.

### RN08 – Convites
- **CA08.1** Dado um convite com mais de 48 h, quando é aceite, então recebe 422 `INVALID_INVITATION` e a membership continua `INVITED`.
- **CA08.2** Dado um convite já aceite, quando o mesmo token é usado de novo, então recebe 422 `INVALID_INVITATION`.
- **CA08.3** Dado um convite reenviado, quando o token anterior é usado, então recebe 422 `INVALID_INVITATION` e o novo token funciona.
- **CA08.4** Dado um token válido, quando é aceite sem OTP válido, então a membership não é activada e a palavra-passe não é guardada.
- **CA08.5** Dado um convite criado, então a base de dados guarda só o hash do token.

### RN09 – MFA obrigatório
- **Removida** (ADR 0019, migration `000036`): todas as contas entram com telefone e palavra-passe, sem segundo factor.

### RN10 – Só ACTIVE autentica
- **CA10.1** Dada uma membership `SUSPENDED`, `INVITED` ou `DEACTIVATED`, quando o utilizador faz login, então não recebe sessão e a resposta é a mesma de credenciais inválidas.

### RN11 – Auditoria
- **CA11.1** Dada qualquer acção deste módulo bem-sucedida, então existe um registo em `audit_logs` com actor, organização, acção, alvo, estado antes e depois, IP e user agent.
- **CA11.2** Dada uma falha ao gravar a auditoria, quando a acção é executada, então a alteração é desfeita (mesma transacção).

### RN12 – Não revelar contas
- **CA12.1** Dado um telefone inexistente e outro existente com palavra-passe errada, quando se faz login com cada um, então as respostas têm o mesmo código e corpo.
- **CA12.2** Dado um e-mail que já é membro de outra organização, quando um parceiro o convida, então a resposta é igual à de um e-mail novo.

### RN13 – Papel só com permissões do seu âmbito
- **CA13.1** Dado um papel PARTNER, quando se tenta associar-lhe `finance:read` (só PLATFORM), no serviço ou directamente na base de dados, então a operação é recusada.

### RN14 – Outro tenant devolve 404
- **CA14.1** Dado um Proprietário do parceiro A, quando pede `GET /v1/members/{id}` de um membro do parceiro B, então recebe 404 com o mesmo corpo de um id inexistente.
- **CA14.2** Dado um Super Admin com `partners:read`, quando pede `GET /v1/partners/{orgId}/members` do parceiro B, então recebe a lista.
- **CA14.3** Dado um Suporte (PLATFORM, sem `partners:read`), quando pede o mesmo, então recebe 403.

### RN15 – Permissões, não papéis
- **CA15.1** Dado um papel personalizado com `members:read` e outro nome qualquer, quando o membro chama `GET /v1/members`, então é autorizado.

### RN16 – RLS como segunda camada
- **CA16.1** Dada uma transacção do pedido com o contexto do parceiro A, quando se executa uma consulta às memberships sem filtro de organização, então só são devolvidas linhas do parceiro A.
- **CA16.2** Dada uma ligação sem contexto de organização, quando se consultam as tabelas com RLS, então não é devolvida nenhuma linha.

### RN17 – Membro desactivado é final
- **CA17.1** Dado um membro `DEACTIVATED`, quando se tenta reactivá-lo, alterar os papéis ou reenviar o convite, então recebe 409 `INVALID_MEMBER_STATE`.

### Endpoints
- **CAE.1** Para cada endpoint protegido: sem token devolve 401; com um token sem a permissão devolve 403; com um recurso de outro tenant devolve 404.
- **CAE.2** Dado `POST /v1/members/invitations` com `organization_id` no corpo, então o campo é ignorado ou rejeitado, e o membro é sempre criado na organização do actor.

## 13. Fora do âmbito

- Clientes (iris) e entregadores/prestadores (hermes): continuam com acesso por posse.
- Criação, edição e remoção de papéis personalizados por API (o modelo suporta, mas sem endpoints).
- Criação e gestão de organizações parceiras (cadastro do parceiro). Assume-se que a organização já existe (Q7).
- Leitura do audit log no backoffice (RF26, C7).
- SSO ou login social.
- Eliminação física de utilizadores ou memberships.

## 14. Questões em aberto

Não decidi nenhuma destas questões.

| Id | Questão |
|---|---|
| Q1 | Os entregadores (hermes) podem pertencer a um parceiro? Se sim, `providers` precisa de `organization_id` e é preciso definir quem aprova: a AGORA, o parceiro ou ambos. |
| Q2 | Qual é o canal do convite e do OTP: e-mail, SMS ou ambos? A API só tem SMS (CastBrick) e não tem envio de e-mail. |
| Q3 | Que política de palavra-passe se aplica a utilizadores internos (hoje mínimo de 8 caracteres) e quanto duram as sessões (hoje 15 min de acesso e 30 dias de refresh)? Deve ser mais curto para o backoffice? |
| Q4 | **Relação com o que já existe (branch `feat/auth-completion`, sem commits):** já há `users.role` (operator, admin, partner), `/api/v1/admin/users` (listar, criar, bloquear, redefinir senha), o comando `bootstrap-admin` e a tabela `audit_log` (ADR 0013). O RF21 substitui isto? É preciso confirmar se `/admin/users` sai, se os utilizadores staff actuais migram para memberships (admin → Super Admin? operator → Operações? partner → Proprietário de que organização?), e se `audit_log` passa a `audit_logs` ou se mantém o nome e se acrescentam colunas. |
| Q5 | Os caminhos do prompt usam `/v1/...`, mas a API usa `/api/v1/...`. Qual fica? |
| Q6 | Clientes HTTP: a API identifica as apps por `X-Client-Id` (`agora-backoffice`, `agora-partner-panel`, `agora-customer-app`, `agora-provider-app`). Mantêm-se estes ids ou passam a `argus`, `athena`, `iris` e `hermes`? |
| Q7 | Quem cria as organizações PARTNER, e por que endpoint? Sem isso, `POST /v1/partners/{orgId}/members/invitations` não tem organização onde convidar. |
| Q8 | Um membro `DEACTIVATED` pode voltar a ser convidado para a mesma organização? A restrição `unique(user_id, organization_id)` impede uma segunda membership. |
| Q9 | Um utilizador com várias memberships (ex.: membro de dois parceiros, ou da AGORA e de um parceiro): como se escolhe a organização activa no login, e pode trocar sem novo login? |
| Q10 | Quais dos RNF01–RNF20 se aplicam e com que metas (ex.: p95 da autorização, retenção do audit log)? O plano MVP só tem a contagem, não o texto. |
| Q12 | A identidade é partilhada com clientes e prestadores? Hoje `users` guarda todos e o e-mail é opcional. Um cliente da iris pode ser também membro de um parceiro com o mesmo registo `users`? |
| Q13 | O JWT actual leva `sid` (para revogação imediata), `cid` e `role`. O novo leva `membership_id`, `organization_id`, `scope` e `perm_version`. Confirmo que `sid` e `cid` se mantêm, e que `role` deixa de ser usado para utilizadores internos? |
| Q14 | O plano MVP coloca o RF21 no ciclo C7. Antecipá-lo afecta o calendário de outros requisitos? |
| Q15 | O PostgreSQL não aplica RLS ao dono das tabelas a menos que se use `FORCE ROW LEVEL SECURITY`, e a API usa o mesmo utilizador para migrações e para os pedidos. Aceita-se separar um utilizador de migrações de um utilizador de runtime, ou forçar RLS também para o dono? (Decisão técnica do plano, mas afecta a infra.) |
