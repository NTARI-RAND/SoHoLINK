> Tradução comunitária (rascunho) — Política NTARI P2-002, Transmissão Multilíngue Global (Global Multilingual Broadcast). Fonte: CONFORMANCE.md (original em inglês, snapshot de 2026-10-05). Rascunho comunitário assistido por máquina, pendente de revisão por mantenedor regional conforme P2-002 §3.1. As especificações técnicas centrais permanecem em inglês conforme §2.2.
>
> Notou algum erro nesta tradução? Correções de tradução são contribuições
> valiosas e muito bem-vindas: faça um fork do repositório e abra um pull
> request em https://github.com/NTARI-RAND/SoHoLINK.

> O original em inglês é normativo. Esta tradução é oferecida para leitura, não para interpretação: onde ela divergir de `CONFORMANCE.md`, prevalece o inglês. As palavras-chave normativas (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY), os nomes de campos, os rótulos e os identificadores são mantidos em inglês porque constituem o contrato de conformidade.

# Conformidade — Arquitetura de Dupla Face

A autodescrição do repositório nos próprios termos da arquitetura, declarada **antes** de qualquer coisa específica de produto ou de implantação, conforme a regra de ordenação da arquitetura. Toda alegação de conformidade está vinculada ao mecanismo e à verificação que a fazem cumprir, ou é rotulada como provisória (stand-in). Prosa não vinculada é marketing.

A arquitetura é a **Arquitetura de Dupla Face (Janus-Facing Architecture, JFA)** — o documento unificado de arquitetura da NTARI, documentação livre sob o comum AGPL-3.0 do projeto. Ela nomeia papéis, nunca produtos; este repositório declara qual papel preenche.

## Declaração de papel

O SoHoLINK é o **coordenador** de um **substrato** da JFA: orquestração do lado dos nós — reconhecimento de nós por meio de listagens de capacidades, pareamento e agendamento, o ciclo de vida da contratação, declarações de taxas, liquidação em moeda fiduciária, tratamento de disputas — e federação de frontends. O coordenador é um **papel plugável**: qualquer implementação conforme pode substituí-lo, e um frontend pode executar o seu próprio ou fazer o pareamento diretamente com os nós. A garantia do substrato a que ele serve: **coordenação sobre infraestrutura que os participantes podem possuir — nenhum ponto de estrangulamento de hospedagem impossível de remover.**

| Termo deste repositório | Papel na arquitetura |
|---|---|
| Coordinator (este repositório) | o coordenador: orquestração do lado dos nós, plugável, substituível |
| Frontend (Cloudy e outros) | um frontend: o aplicativo voltado aos membros; onde vivem os membros e a economia dos membros |
| Node | infraestrutura de propriedade dos participantes |
| Tabela `participants`, portal, agente | **superfícies de membros transitórias** — capacidades de propriedade do frontend hospedadas aqui enquanto aguardam migração, rotuladas como tal no CLAUDE.md |

## Invariantes e seus vínculos

| Invariante (arquitetura) | Mecanismo aqui | Verificação |
|---|---|---|
| **Pessoas nunca aparecem na rede** | A longo prazo, o coordenador não modela pessoas: as contrapartes são frontends e nós; a identidade na rede é a identidade da carga de trabalho (SPIFFE NodeID) via `sohocloud-protocol` | Protocolo consumido na tag publicada; superfícies de pessoas transitórias rotuladas, não negadas |
| **Identidade única de participante** | Uma única tabela `participants` (migração 011) — não existe divisão entre provedor e consumidor no esquema nem no código | Esquema; um grep por `providers`/`consumers` retorna apenas o histórico de migrações |
| **Legibilidade das taxas** | As taxas existem apenas como mensagens `FeeDeclaration` assinadas pelo coordenador — com autoria, legíveis, contestáveis | Pacote `fees/` do protocolo + SPEC |
| **Moeda fiduciária permanece fiduciária; o crédito permanece em casa** | A liquidação é puramente em moeda fiduciária (Stripe Connect); sem tokens, sem carteiras; estritamente separada de qualquer crédito emitido por membros de um frontend | Invariante do CLAUDE.md; o pacote de pagamento lida apenas com o Stripe |
| **Nenhuma assimetria de informação** | Preços, medição e ganhos visíveis a todos os participantes | Superfícies do portal; tabelas de medição |
| **Superfície de governança separada** | Portal de administração em uma porta separada, somente local, nunca exposta publicamente, e não como flags de papel no portal público | Configuração de implantação |
| **Proveniência: entrada = saída** | AGPL-3.0; DCO (`Signed-off-by` em cada commit), sem CLA | Licença; workflow de DCO (ver rótulos provisórios: aplicação suspensa como interinidade nomeada) |

## Rótulos provisórios e resíduos em aberto

- **Superfícies de membros transitórias.** O agente de nó, o portal de membros, a tabela `participants` e o instalador são capacidades de propriedade do frontend hospedadas aqui, herdadas da era de papel duplo, enquanto aguardam migração. Elas são rotuladas, mantidas em funcionamento e nunca descritas como o papel de longo prazo do coordenador.
- **A federação de coordenadores é uma meta de projeto, não está construída.** Checkpoints testemunhados (o modelo de transparência de certificados reservado na camada de registro e o stub `anchor/` do protocolo) são a meta nomeada para a não equivocação entre coordenadores. Até lá, esta é uma implantação de coordenador único: o problema em aberto 7 da arquitetura — a computação soberana compra uma saída mecânica, não econômica — aplica-se integralmente e é nomeado, não resolvido.
- **Autenticação do frontend como operador — CONSTRUÍDA na rede de coordenação.** A superfície `/v0` do lado dos nós e o cadastro de chave pública de nó agora aceitam uma transmissão de operador 2-de-7 (conjunto rotativo de sete chaves, cabeçalho `X-SohoCloud-Operator`, janela de replay por `(operator,coordinator)`) por meio de um seletor operador-ou-SPIFFE: o cabeçalho seleciona o caminho do operador; sua ausência mantém o caminho SPIFFE existente para nós diretos/satélites. A autenticidade do nó em ambos os caminhos vem da própria assinatura ed25519 de cada mensagem (verificada em relação a `node_protocol_keys`), de modo que o caminho de retransmissão pelo operador não acrescenta nenhum SVID por nó. Verificação: testes unitários de `OperatorAuth`/`OperatorOrSPIFFE`; Suite B do harness de conformidade. Resíduo: nenhum operador concluiu ainda o cadastro de ponta a ponta (bloqueado no transporte de e-mail do 2FA), portanto o caminho está verificado em teste, mas ainda não em produção.
- **Contraparte da liquidação em moeda fiduciária** (a identidade de recebimento do membro vs. o frontend) é uma questão deliberadamente em aberto; qualquer que seja a resposta, ela permanece do lado da moeda fiduciária, nunca confundida com o crédito dos membros.
- **A aplicação do DCO está suspensa como interinidade nomeada (2026-07-12).** O workflow `dco.yml` está desativado — não excluído — durante o esforço de estabelecimento da pilha, por decisão do administrador responsável (steward). Conforme o padrão de proveniência da arquitetura: o pacto em si nunca é suspenso (as contribuições continuam entrando no comum AGPL com entrada = saída, sem efeito sobre o Contributor Covenant); a reativação está assumida como compromisso e é uma única ação (`gh workflow enable DCO`); e a lacuna é honesta — commits feitos durante a janela não podem ser certificados retroativamente, por isso a janela é mantida curta e seu início e seu fim são matéria de registro. Commits de autoria de agentes continuam a levar `Signed-off-by` durante toda a janela, independentemente disso.

## Declaração de dependências

Consome `sohocloud-protocol` em uma tag de versão publicada — a folha de dependência compartilhada tanto por este coordenador quanto pelos frontends. Não depende de nenhum frontend nem de nenhum código da economia dos membros; a economia dos membros vive acima desta camada e não deve migrar para baixo, para dentro dela.

## Notas específicas do produto (por último, conforme a regra de ordenação)

Implantação de referência em soholink.org; domínio de confiança `spiffe://soholink.org`. O Cloudy é o frontend de referência. Nenhum dos dois é privilegiado pelo protocolo.
