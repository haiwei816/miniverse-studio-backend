import hashlib
import json
from pathlib import Path
from PIL import Image

ASSETS_DIR = Path(__file__).resolve().parent.parent / "player" / "web" / "public" / "games" / "witch-cauldron" / "assets"

THEME_MAX_EDGE = {
    "background": 1200,
    "title": 1200,
    "workbench": 1200,
    "cauldron": 960,
    "liquid": 960,
    "reaper": 960,
    "victory": 960,
    "scroll": 960,
    "cat": 960,
    "brew-button": 960,
}


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(65536), b""):
            h.update(chunk)
    return h.hexdigest()


def convert_image(src: Path, dst: Path, max_edge: int | None = None, lossless: bool = False, quality: int = 86) -> None:
    with Image.open(src) as img:
        if img.mode not in ("RGBA", "RGB"):
            img = img.convert("RGBA")
        if max_edge and max(img.width, img.height) > max_edge:
            img.thumbnail((max_edge, max_edge), Image.Resampling.LANCZOS)
        save_kwargs = {"format": "WEBP", "method": 6}
        if lossless:
            save_kwargs["lossless"] = True
        else:
            save_kwargs["quality"] = quality
        img.save(dst, **save_kwargs)


def main() -> None:
    manifest_path = ASSETS_DIR / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    provenance_path = ASSETS_DIR / "provenance.json"
    provenance = json.loads(provenance_path.read_text(encoding="utf-8"))

    before_bytes = sum(p.stat().st_size for p in ASSETS_DIR.rglob("*") if p.is_file())
    converted = 0
    path_map: dict[str, str] = {}

    for group, entries in manifest.items():
        if group == "audio" or not isinstance(entries, dict):
            continue
        for key, rel in list(entries.items()):
            rel_path = rel.removeprefix("assets/")
            src = ASSETS_DIR / rel_path
            dst_rel = Path(rel_path).with_suffix(".webp").as_posix()
            dst = ASSETS_DIR / dst_rel
            if src.exists() and src.suffix.lower() == ".png":
                if group == "symbols":
                    convert_image(src, dst, max_edge=192, quality=78)
                elif group == "relics":
                    convert_image(src, dst, max_edge=128, lossless=True)
                else:
                    max_edge = THEME_MAX_EDGE.get(key, 960)
                    quality = 78
                    convert_image(src, dst, max_edge=max_edge, quality=quality)
                src.unlink()
                converted += 1
            elif not dst.exists():
                raise FileNotFoundError(f"Missing asset for {rel}")
            new_manifest_rel = f"assets/{dst_rel}"
            entries[key] = new_manifest_rel
            path_map[rel] = new_manifest_rel

    manifest_path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (ASSETS_DIR / "manifest.js").write_text(
        "globalThis.CauldronAssets = " + json.dumps(manifest, ensure_ascii=False) + ";\n",
        encoding="utf-8",
    )

    for item in provenance.get("assets", []):
        dest = item.get("destination", "")
        if dest in path_map:
            dest = path_map[dest]
            item["destination"] = dest
        target = ASSETS_DIR.parent / dest
        if target.exists():
            item["sha256"] = sha256_file(target)
    provenance_path.write_text(json.dumps(provenance, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    after_bytes = sum(p.stat().st_size for p in ASSETS_DIR.rglob("*") if p.is_file())
    print(f"Converted {converted} PNG assets to WebP: {before_bytes / 1024 / 1024:.2f} MiB -> {after_bytes / 1024 / 1024:.2f} MiB")


if __name__ == "__main__":
    main()
