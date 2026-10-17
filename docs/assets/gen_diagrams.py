#!/usr/bin/env python3
"""Generates the README architecture diagram (light and dark SVG).

Run from the repo root:  python3 docs/assets/gen_diagrams.py
Every component drawn here exists in the code: ports and routes in
cmd/vindice/main.go, tables in internal/adapter/out/clickhouse/schema.
"""
from pathlib import Path
from xml.sax.saxutils import escape

OUT = Path(__file__).parent

THEMES = {
    "light": dict(bg="#ffffff", box="#f6f7f9", stroke="#d0d4dc", text="#1b1e24", muted="#5f6673",
                  accent="#6a5ae0", accent_box="#f1effd", store="#0e9aa7", store_box="#e9f7f8", arrow="#8a91a0"),
    "dark": dict(bg="#0f1115", box="#161920", stroke="#2c313b", text="#e6e8ec", muted="#9aa2b0",
                 accent="#8f82ff", accent_box="#1c1a33", store="#3cc8d4", store_box="#11262a", arrow="#6b7385"),
}

W, H = 1170, 500
BW = 190  # box width

# (x, y, h, kind, title, lines); kind: src | vindice | store | ext
BOXES = {
    "sdk":      (24, 62, 50, "src", "Go SDK", ["vindice.Start(...)"]),
    "otel":     (24, 122, 50, "src", "OpenTelemetry", ["any SDK, Collector, Alloy"]),
    "prom":     (24, 182, 50, "src", "Prometheus", ["remote write 1.0"]),
    "ingest":   (256, 62, 170, "vindice", "vindice ingest", [
        ":4317  OTLP gRPC", ":4318  OTLP HTTP", "/api/v1/write  remote write", "API key check (vdc_...)"]),
    "kafka":    (488, 62, 170, "store", "Kafka", [
        "durable buffer", "ack only after write", "replay after outages", "zstd compressed"]),
    "writer":   (720, 62, 170, "vindice", "vindice writer", [
        "drop rules", "tail sampling", "RED before sampling", "dedupe, large batches"]),
    "ch":       (952, 62, 170, "store", "ClickHouse", [
        "logs, spans, metrics", "1m / 1h rollups", "per-severity retention", "token + bloom indexes"]),
    "alert":    (720, 282, 92, "vindice", "vindice alert", ["rules every 30s", "ok, pending, firing"]),
    "query":    (952, 282, 92, "vindice", "vindice query  :8080", ["REST API + embedded UI", "login, roles"]),
    "telegram": (720, 420, 54, "ext", "Telegram", ["firing / resolved"]),
    "browser":  (952, 420, 54, "ext", "Your team", ["browser"]),
}

# (from, to, label); arrows leave the right/bottom edge and enter left/top.
ARROWS = [
    ("sdk", "ingest", ""), ("otel", "ingest", ""), ("prom", "ingest", ""),
    ("ingest", "kafka", ""), ("kafka", "writer", ""), ("writer", "ch", ""),
    ("ch", "query", ""), ("ch", "alert", ""), ("alert", "telegram", ""), ("query", "browser", ""),
]


def anchor(name, side):
    x, y, h = BOXES[name][:3]
    return {"right": (x + BW, y + h / 2), "left": (x, y + h / 2),
            "top": (x + BW / 2, y), "bottom": (x + BW / 2, y + h)}[side]


def legend(c):
    """Colour key in the empty bottom-left corner."""
    rows = [(c["accent_box"], c["accent"], "vindice: one Go binary, run as all, ingest,"),
            (None, None, "writer, query or alert"),
            (c["store_box"], c["store"], "Storage: Kafka buffer, ClickHouse"),
            (c["box"], c["stroke"], "Outside Vindice")]
    out, y = [], 300
    for fill, stroke, label in rows:
        if fill:
            out.append(f'<rect x="24" y="{y - 11}" width="14" height="14" rx="3" fill="{fill}" '
                       f'stroke="{stroke}" stroke-width="1.5"/>')
        out.append(f'<text x="48" y="{y}" font-size="12.5" fill="{c["muted"]}">{escape(label)}</text>')
        y += 24 if fill else 30
    return out


def render(theme):
    c = THEMES[theme]
    out = [f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" '
           f'font-family="-apple-system, Segoe UI, Helvetica, Arial, sans-serif">',
           f'<defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" '
           f'orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="{c["arrow"]}"/></marker></defs>',
           f'<rect width="{W}" height="{H}" fill="{c["bg"]}"/>',
           f'<text x="24" y="38" font-size="17" font-weight="700" fill="{c["text"]}">Vindice architecture</text>']
    out += legend(c)

    for a, b, label in ARROWS:
        if BOXES[b][1] > BOXES[a][1] + BOXES[a][2] - 1:  # target below
            (x1, y1), (x2, y2) = anchor(a, "bottom"), anchor(b, "top")
            if abs(x1 - x2) > 1:  # elbow: down, across, down
                my = (y1 + y2) / 2
                d = f"M{x1},{y1} V{my} H{x2} V{y2 - 2}"
            else:
                d = f"M{x1},{y1} V{y2 - 2}"
        else:
            (x1, y1), (x2, y2) = anchor(a, "right"), anchor(b, "left")
            mx = (x1 + x2) / 2
            d = f"M{x1},{y1} H{mx} V{y2} H{x2 - 2}" if abs(y1 - y2) > 1 else f"M{x1},{y1} H{x2 - 2}"
        out.append(f'<path d="{d}" fill="none" stroke="{c["arrow"]}" stroke-width="1.6" marker-end="url(#arrow)"/>')
        if label:
            out.append(f'<text x="{(x1 + x2) / 2}" y="{y1 - 6}" font-size="11" text-anchor="middle" '
                       f'fill="{c["muted"]}">{escape(label)}</text>')

    for name, (x, y, h, kind, title, lines) in BOXES.items():
        fill, stroke = {"vindice": (c["accent_box"], c["accent"]), "store": (c["store_box"], c["store"])}.get(
            kind, (c["box"], c["stroke"]))
        width = 2 if kind in ("vindice", "store") else 1
        out.append(f'<rect x="{x}" y="{y}" width="{BW}" height="{h}" rx="9" fill="{fill}" '
                   f'stroke="{stroke}" stroke-width="{width}"/>')
        title_color = c["accent"] if kind == "vindice" else c["store"] if kind == "store" else c["text"]
        out.append(f'<text x="{x + 14}" y="{y + 24}" font-size="14" font-weight="700" '
                   f'fill="{title_color}">{escape(title)}</text>')
        for i, line in enumerate(lines):
            out.append(f'<text x="{x + 14}" y="{y + 46 + i * 20}" font-size="12.5" '
                       f'fill="{c["muted"] if kind != "vindice" else c["text"]}">{escape(line)}</text>')

    out.append("</svg>")
    return "\n".join(out) + "\n"


if __name__ == "__main__":
    for theme in THEMES:
        path = OUT / f"architecture-{theme}.svg"
        path.write_text(render(theme))
        print(path)
