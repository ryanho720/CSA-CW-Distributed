import csv
import matplotlib

matplotlib.use("Agg")  # headless backend to avoid GUI hangs
import matplotlib.pyplot as plt
import pandas as pd
import seaborn as sns


def load_benchstat_csv(path: str) -> pd.DataFrame:
    """Parse benchstat -format csv output into a DataFrame."""
    records = []
    metric = None
    with open(path, newline="") as f:
        reader = csv.reader(f)
        for row in reader:
            if not row or all(cell == "" for cell in row):
                continue
            if row[0].startswith(("goos:", "goarch:", "pkg:", "cpu:")):
                continue
            if row[0] == "" and len(row) >= 3 and row[2] == "CI":
                metric = row[1]
                continue
            if row[0] == "" or row[0].startswith("geomean"):
                continue
            if metric:
                try:
                    value = float(row[1])
                except ValueError:
                    continue
                records.append({"name": row[0], "metric": metric, "value": value})
    return pd.DataFrame(records)


def parse_threads_benchmark(name: str) -> tuple[str, int]:
    """Extract (label, workers) from BenchmarkRunTurnThreads/64x64_threads_4-8."""
    base = name
    if "-" in base:
        base = base.rsplit("-", 1)[0]  # drop "-8"
    # expect BenchmarkRunTurnThreads/<size>_threads_<n>
    parts = base.split("/")
    if len(parts) >= 2 and parts[0].startswith("BenchmarkRunTurnThreads"):
        variant = parts[1]
        if "_threads_" in variant:
            size, workers = variant.split("_threads_", 1)
            try:
                workers = int(workers)
            except ValueError:
                workers = None
            return size, workers
    return ("unknown", None)


def save_barplot(df: pd.DataFrame, x: str, y: str, title: str, xlabel: str, filename: str):
    if df.empty:
        return
    sns.barplot(data=df, x=x, y=y)
    plt.ylabel("Time per turn (ms)")
    plt.xlabel(xlabel)
    plt.title(title)
    plt.tight_layout()
    plt.savefig(filename)
    plt.clf()


def main():
    df = load_benchstat_csv("runturn.csv")
    df = df[df["metric"] == "ms/op"].copy()
    if df.empty:
        raise SystemExit("No ms/op entries found in runturn.csv")

    labels, workers = zip(*(parse_threads_benchmark(n) for n in df["name"]))
    df["label"] = labels
    df["workers"] = workers
    df = df.dropna(subset=["workers"])
    df["workers"] = df["workers"].astype(int)
    df = df.rename(columns={"value": "time_ms"})

    # One CSV per size plus a combined CSV, and a plot per size.
    df.to_csv("runturn_all.csv", index=False)
    for label, sub in df.groupby("label"):
        sub = sub.sort_values("workers")
        sub.to_csv(f"runturn_{label}.csv", index=False)
        save_barplot(sub, "workers", "time_ms", f"RunTurn {label}", "Worker threads", f"runturn_{label}.png")


if __name__ == "__main__":
    main()
