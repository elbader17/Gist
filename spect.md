Gist - Especificación Técnica
Sistema de Optimización de Contexto y Gestión Eficiente de Tokens para Servidores MCP

    Filosofía del Proyecto

El avance de las herramientas de desarrollo basadas en agentes autónomos (como Claude Code, OpenCode o pasarelas avanzadas) ha revelado un nuevo cuello de botella crítico: la saturación innecesaria del contexto de los LLMs. Los agentes tienden a leer archivos enteros de miles de líneas de código, inyectar logs masivos de compilación o entrar en bucles infinitos de prueba y error, provocando un consumo exponencial de tokens y costos prohibitivos.

Gist nace como una herramienta CLI de alta performance desarrollada en Go, diseñada para actuar como un servidor MCP independiente. Su único objetivo es interceptar, filtrar y reestructurar la información antes de que sea enviada al LLM, sirviendo como la capa de eficiencia definitiva del ecosistema de desarrollo.

Principios Fundamentales:

    Poda Semántica sobre Lectura Cruda: Nunca entregar texto completo si un resumen o estructura sintáctica es suficiente.

    Previsibilidad de Costos: Detener los bucles automáticos destructivos antes de que consuman el presupuesto del usuario.

    Maximización de Caching: Forzar la estructura del contexto para aprovechar al 100% las capacidades de Prompt Caching de los proveedores modernos (Anthropic, Google, OpenAI).

    Arquitectura del Sistema

Al ser una herramienta CLI en Go, TokenLess se ejecuta como un binario nativo ultra-ligero que se comunica mediante la entrada y salida estándar (stdin/stdout) utilizando el protocolo JSON-RPC de MCP.

Esquema de flujo:
Cliente (Claude Code / OpenCode) -> stdio (JSON-RPC) -> TokenLess Server (Go CLI) -> Archivos Locales / Git Diff

Módulos Internos del Servidor TokenLess:

    BPE Tokenizer (Conteo local)

    AST Engines (Poda nativa en Go y generación de esqueletos)

    Budget Circuit (Interruptor de bucles)

    Context Aligner (Optimización para caché)

Componentes de Software (Módulos Go)

    /cmd/tokenless/main.go: Punto de entrada del CLI, inicializa el protocolo de comunicación MCP y rutea los comandos stdio.

    /pkg/ast/: Parsers sintácticos utilizando la librería estándar de Go (go/ast, go/parser) para procesar código y generar esqueletos colapsados.

    /pkg/tokenizer/: Lógica de conteo de tokens offline utilizando codificación BPE (compatible con CL100K_Base / O200K_Base) para calcular pesos exactos localmente sin latencia de red.

    /pkg/budget/: Base de datos embebida ligera o almacenamiento en memoria/JSON para el control transaccional del consumo de tokens por sesión de desarrollo.

    Catálogo de Herramientas MCP (Specs de Implementación)

Gist expondrá un set de herramientas clave que sustituirán de manera eficiente las acciones por defecto de los agentes autónomos.

3.1. Herramienta: view_file_slim
Propósito: Sustituir comandos genéricos de lectura (cat, less) entregando una versión podada sintácticamente del archivo.

    Parámetros de Entrada:

        file_path: string (Ruta del archivo)

        focus_functions: array de strings (Opcional: funciones que no se deben colapsar)

        max_lines_body: int (Por defecto: 0. Número de líneas que preserva de cada cuerpo)

    Lógica Interna (Go AST Parsing):

        Abre y mapea el archivo en memoria.

        Si es un archivo .go, utiliza go/parser para construir el árbol de sintaxis abstracta.

        Identifica todos los nodos de tipo FuncDecl (Declaraciones de funciones), TypeSpec (Estructuras e Interfaces) e ImportSpec.

        Para toda función que no esté listada en focus_functions, reemplaza el bloque de sentencias (BlockStmt) por una cadena única inyectada: // ... [Cuerpo colapsado por TokenLess para optimizar contexto] ...

        Mantiene intactas las firmas, tipos de retorno, estructuras de datos y comentarios de cabecera.

    Resultado: Reduce el peso en tokens del archivo hasta en un 90-95%, permitiendo al agente comprender dependencias y firmas sin pagar el costo de la lógica interna repetitiva.

3.2. Herramienta: enforce_budget
Propósito: Actuar como interruptor de seguridad (disyuntor) en flujos de corrección iterativa o bucles de agentes.

    Parámetros de Entrada:

        session_id: string (Identificador único de la sesión de terminal)

        current_action: string (Comando o cambio de código que el agente intenta realizar)

        estimated_tokens: int (Tokens consumidos por el último payload)

    Lógica Interna:

        Carga el estado de la sesión guardado localmente en ~/.config/tokenless/sessions.json.

        Incrementa el contador acumulativo de tokens y costo financiero aproximado.

        Ejecuta un algoritmo de detección de patrones repetitivos (si la misma acción o comando de testeo fallido se ejecuta más de 3 veces consecutivas).

        Evalúa si el consumo de la sesión superó el límite establecido por el desarrollador (ej. $2.00 USD o 500,000 tokens).

        Si se violan los umbrales, el comando retorna un código de error JSON-RPC controlado que interrumpe la ejecución del agente, imprimiendo un mensaje explícito en la consola del usuario para requerir autorización manual.

3.3. Herramienta: align_context_cache
Propósito: Reorganizar la estructura de los payloads para garantizar que se dispare el Prompt Caching del LLM de forma óptima.

    Parámetros de Entrada:

        system_prompts: array de strings

        static_files_context: array de strings (Contenido de archivos estáticos)

        dynamic_input: string (El error actual, comando reciente o instrucción variable)

    Lógica Interna:

        El prompt caching requiere bloques estáticos idénticos al inicio del prompt de la API (mínimo de 1024 a 2048 tokens dependiendo del proveedor).

        TokenLess toma todos los componentes del contexto y los ordena estrictamente en capas fijas de arriba hacia abajo:

            Capa 1: Reglas fijas y prompts del sistema.

            Capa 2: Esqueletos de arquitectura y archivos estáticos (ordenados alfabéticamente por ruta para mantener consistencia exacta de bytes).

            Capa 3: Historial comprimido de la conversación previa.

            Capa 4 (Última línea): Datos variables, errores en tiempo de ejecución o respuestas inmediatas.

        Devuelve el payload optimizado en bloques limpios listos para su serialización.

3.4. Herramienta: fetch_diff_context
Propósito: Reemplazar el uso masivo de git diff crudo por una abstracción semántica compacta de las modificaciones.

    Parámetros de Entrada:

        target_branch: string (Por defecto: HEAD o main)

    Lógica Interna:

        Ejecuta de forma nativa comandos git mediante os/exec en Go para obtener el diff.

        Analiza las líneas agregadas (+) y eliminadas (-).

        En lugar de escupir todo el archivo modificado con metadatos repetitivos de Git, parsea las líneas afectadas y genera un resumen tipificado:

            Identifica qué funciones sufrieron alteraciones en su firma o lógica interna.

            Detecta si solo se agregaron comentarios o logs de depuración (fmt.Println).

        Retorna una estructura limpia: [Archivo] -> [Función Modificada] -> Breve huella del cambio realizado.

    Almacenamiento Local y Configuración

TokenLess mantendrá un archivo de configuración JSON/YAML local de baja latencia en el directorio base del usuario:

Ruta: ~/.config/tokenless/config.json
Contenido por defecto:

    max_session_cost_usd: 2.00

    default_tokenizer_encoding: "cl100k_base"

    loop_detection_threshold: 3

    cache_alignment_enabled: true

    pricing: prompt_per_million: 3.00, completion_per_million: 15.00, cached_prompt_per_million: 0.30

    Criterios de Performance en Go

Para garantizar que esta herramienta de control no agregue latencia al pipeline de desarrollo del usuario, se aplicarán las siguientes directrices en la implementación en Go:

    Cero Alojamientos Innecesarios en Memoria (Zero-Allocation): Utilizar sync.Pool para la reutilización de buffers al escanear y tokenizar archivos de texto grandes.

    Estructuras de Lectura Eficientes: Emplear bufio.Scanner y lectores basados en streams para el procesamiento de archivos por fragmentos en lugar de cargar archivos masivos enteros en strings.

    Compilación Estática: Generar un binario único autocontenido sin dependencias dinámicas externas de C (CGO_ENABLED=0), facilitando la instalación instantánea mediante un simple comando go install.
