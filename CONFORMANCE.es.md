> Traducción comunitaria (borrador) — Política P2-002 de NTARI, Difusión Multilingüe Global. Fuente: CONFORMANCE.md (original en inglés, instantánea del 2026-10-05). Borrador comunitario asistido por máquina, pendiente de revisión por mantenedores regionales conforme a P2-002 §3.1. Las especificaciones técnicas centrales permanecen en inglés conforme a §2.2.
>
> ¿Encontraste un error en esta traducción? Tu corrección es una contribución
> bienvenida y valorada: haz un fork del repositorio y abre un pull request en
> https://github.com/NTARI-RAND/SoHoLINK.

> **El original en inglés es normativo.** Esta traducción se ofrece para lectura, no para interpretación: donde difiera de `CONFORMANCE.md`, rige el inglés. Las palabras clave normativas (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY), los nombres de campos, las etiquetas y los identificadores se mantienen en inglés porque constituyen el contrato de conformidad.

# Conformidad — Janus-Facing Architecture

La autodescripción del repositorio en los propios términos de la arquitectura, expuesta **antes** de cualquier cosa específica del producto o del despliegue, según la regla de orden de la arquitectura. Cada afirmación de conformidad está vinculada al mecanismo y a la verificación que la hacen cumplir; de lo contrario, se etiqueta como suplencia. La prosa no vinculada es marketing.

La arquitectura es **Janus-Facing Architecture (JFA)**: el documento de arquitectura unificada de NTARI, documentación libre bajo el bien común AGPL-3.0 del proyecto. Nombra roles, nunca productos; este repositorio declara qué rol ocupa.

## Declaración de rol

SoHoLINK es el **coordinador** de un **sustrato** de JFA: orquestación del lado de los nodos — reconocimiento de nodos mediante listados de capacidad, emparejamiento y programación, el ciclo de vida del empleo, declaraciones de tarifas, liquidación en dinero fiat, gestión de disputas — y federación de frontends. El coordinador es un **rol intercambiable**: cualquier implementación conforme puede reemplazarlo, y un frontend puede ejecutar el suyo propio o emparejar directamente con los nodos. La garantía del sustrato a la que sirve: **coordinación sobre infraestructura que los participantes pueden poseer — sin un punto de estrangulamiento de alojamiento imposible de eliminar.**

| Término de este repositorio | Rol en la arquitectura |
|---|---|
| Coordinator (este repositorio) | el coordinador: orquestación del lado de los nodos, intercambiable, reemplazable |
| Frontend (Cloudy y otros) | un frontend: la aplicación de cara a los miembros; donde viven los miembros y la economía de miembros |
| Node | infraestructura propiedad de los participantes |
| Tabla `participants`, portal, agente | **superficies de miembros transitorias** — capacidades propiedad de los frontends alojadas aquí a la espera de su migración, etiquetadas como tales en CLAUDE.md |

## Invariantes y sus vínculos

| Invariante (arquitectura) | Mecanismo aquí | Verificación |
|---|---|---|
| **Las personas nunca aparecen en el canal** | A largo plazo, el coordinador no modela personas: las contrapartes son frontends y nodos; la identidad en el canal es identidad de carga de trabajo (NodeID de SPIFFE) mediante `sohocloud-protocol` | Protocolo consumido en una etiqueta publicada; las superficies transitorias con personas están etiquetadas, no negadas |
| **Identidad única de participante** | Una sola tabla `participants` (migración 011) — no existe ninguna separación proveedor/consumidor en el esquema ni en el código | Esquema; una búsqueda con grep de `providers`/`consumers` solo devuelve el historial de migraciones |
| **Legibilidad de las tarifas** | Las tarifas existen solo como mensajes `FeeDeclaration` firmados por el coordinador — con autoría, legibles, impugnables | Paquete `fees/` del protocolo + SPEC |
| **El fiat sigue siendo fiat; el crédito se queda en casa** | La liquidación es puramente en dinero fiat (Stripe Connect); sin tokens, sin billeteras; estrictamente separada del crédito emitido por los miembros de cualquier frontend | Invariante de CLAUDE.md; el paquete de pagos solo interactúa con Stripe |
| **Sin asimetría de información** | Precios, medición y ganancias visibles para todos los participantes | Superficies del portal; tablas de medición |
| **Superficie de gobernanza separada** | Portal de administración en un puerto separado solo local, nunca expuesto públicamente, no como indicadores de rol en el portal público | Configuración del despliegue |
| **Procedencia de entrada = de salida** | AGPL-3.0; DCO (`Signed-off-by` en cada commit), sin CLA | Licencia; flujo de trabajo de DCO (véanse las suplencias: aplicación suspendida como medida provisional declarada) |

## Suplencias y cuestiones residuales abiertas

- **Superficies de miembros transitorias.** El agente de nodo, el portal de miembros, la tabla `participants` y el instalador son capacidades propiedad de los frontends alojadas aquí desde la era de doble rol, a la espera de su migración. Están etiquetadas, se mantienen en funcionamiento y nunca se describen como el rol a largo plazo del coordinador.
- **La federación de coordinadores es un objetivo de diseño, no está construida.** Los checkpoints atestiguados (el modelo de transparencia de certificados reservado en la capa de registro y el stub `anchor/` del protocolo) son el objetivo nombrado para la no equivocación entre coordinadores. Hasta entonces, este es un despliegue de un solo coordinador: el problema abierto 7 de la arquitectura — el cómputo soberano otorga una salida mecánica, no económica — se aplica plenamente y está nombrado, no resuelto.
- **Autenticación de frontend-como-operador — CONSTRUIDA sobre el canal de coordinación.** La superficie `/v0` del lado de los nodos y la inscripción de claves públicas de nodos ahora aceptan una transmisión de operador 2-de-7 (conjunto rotativo de siete claves, encabezado `X-SohoCloud-Operator`, ventana de repetición por `(operator,coordinator)`) mediante un selector de operador-o-SPIFFE: el encabezado selecciona la ruta del operador; su ausencia mantiene la ruta SPIFFE existente para nodos directos/satélite. La autenticidad de los nodos en ambas rutas proviene de la propia firma ed25519 de cada mensaje (verificada contra `node_protocol_keys`), por lo que la ruta de relevo del operador no añade ningún SVID por nodo. Verificación: pruebas unitarias de `OperatorAuth`/`OperatorOrSPIFFE`; Suite B del arnés de conformidad. Cuestión residual: ningún operador ha completado todavía la inscripción de extremo a extremo (bloqueada por el transporte de correo de 2FA), por lo que la ruta está verificada en pruebas, aún no en producción.
- **La contraparte de la liquidación en fiat** (identidad de cobro del miembro frente al frontend) es una cuestión deliberadamente abierta; cualquiera de las dos respuestas permanece del lado del fiat, nunca confundida con el crédito de los miembros.
- **La aplicación del DCO está suspendida como medida provisional declarada (2026-07-12).** El flujo de trabajo `dco.yml` está deshabilitado — no eliminado — para el impulso de establecimiento de la pila, por decisión del custodio (steward). Según el estándar de procedencia de la arquitectura: el pacto en sí nunca se suspende (las contribuciones siguen entrando en el bien común AGPL con entrada = salida, el Contributor Covenant no se ve afectado); la reinstauración es un compromiso firme y consiste en una sola acción (`gh workflow enable DCO`); y la brecha es honesta — los commits realizados durante la ventana no pueden certificarse retroactivamente, por lo que la ventana se mantiene corta y su inicio y su fin constan en el registro. Los commits creados por agentes siguen llevando `Signed-off-by` durante toda la ventana, en cualquier caso.

## Declaración de dependencias

Consume `sohocloud-protocol` en una etiqueta de versión publicada — la dependencia hoja que comparten tanto este coordinador como los frontends. No depende de ningún frontend ni de ningún código de la economía de miembros; la economía de miembros vive por encima de esta capa y no debe migrar hacia abajo dentro de ella.

## Notas específicas del producto (al final, según la regla de orden)

Despliegue de referencia en soholink.org; dominio de confianza `spiffe://soholink.org`. Cloudy es el frontend de referencia. Ninguno de los dos es privilegiado por el protocolo.
