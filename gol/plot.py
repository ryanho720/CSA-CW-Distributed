import csv
import os
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
    df = pd.DataFrame(records)
    # normalize metrics to ms/op
    if not df.empty:
        def to_ms(row):
            if row["metric"] == "ms/op":
                return row["value"], "ms/op"
            if row["metric"] == "sec/op":
                return row["value"] * 1000.0, "ms/op"
            if row["metric"] == "ns/op":
                return row["value"] / 1_000_000.0, "ms/op"
            return row["value"], row["metric"]

        converted = df.apply(lambda r: pd.Series(to_ms(r), index=["value", "metric"]), axis=1)
        df["value"] = converted["value"]
        df["metric"] = converted["metric"]
    return df


def parse_threads_benchmark(name: str) -> tuple[str, int]:
    """Extract (label, workers) from local or remote vary-workers benchmarks."""
    base = name
    if "-" in base:
        base = base.rsplit("-", 1)[0]  # drop "-8"
    # expect BenchmarkRunTurnThreads/<size>_threads_<n> or BenchmarkRemoteProcess_VaryWorkers/<n>_workers
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
    if len(parts) >= 2 and (parts[0].startswith("BenchmarkRemoteProcess_VaryWorkers") or parts[0].startswith("RemoteProcess_VaryWorkers")):
        variant = parts[1]
        if "_workers" in variant:
            return "Remote (512x512,1 turn)", int(variant.split("_workers", 1)[0])
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
    # Local benchmarks
    if os.path.exists("runturn.csv"):
        df_local = load_benchstat_csv("runturn.csv")
        if "metric" not in df_local.columns:
            print("runturn.csv missing metric column; skipping local plots")
        else:
            df_local = df_local[df_local["metric"] == "ms/op"].copy()
            if df_local.empty:
                print("No ms/op entries found in runturn.csv")
            else:
                labels, workers = zip(*(parse_threads_benchmark(n) for n in df_local["name"]))
                df_local["label"] = labels
                df_local["workers"] = workers
                df_local = df_local.dropna(subset=["workers"])
                df_local["workers"] = df_local["workers"].astype(int)
                df_local = df_local.rename(columns={"value": "time_ms"})

                df_local.to_csv("runturn_all.csv", index=False)
                for label, sub in df_local.groupby("label"):
                    sub = sub.sort_values("workers")
                    sub.to_csv(f"runturn_{label}.csv", index=False)
                    title = "RunTurn: Varying workers" if label != "unknown" else "RunTurn"
                    save_barplot(sub, "workers", "time_ms", title, "Worker threads", f"runturn_{label}.png")
    else:
        print("runturn.csv not found; skipping local plots")

    # Remote benchmarks (optional)
    if os.path.exists("runturn_remote.csv"):
        df_remote = load_benchstat_csv("runturn_remote.csv")
        if "metric" not in df_remote.columns:
            print("runturn_remote.csv missing metric column; skipping remote plots")
        else:
            df_remote = df_remote[df_remote["metric"] == "ms/op"].copy()
            if not df_remote.empty:
                labels, workers = zip(*(parse_threads_benchmark(n) for n in df_remote["name"]))
                df_remote["label"] = labels
                df_remote["workers"] = workers
                df_remote = df_remote.dropna(subset=["workers"])
                df_remote["workers"] = df_remote["workers"].astype(int)
                df_remote = df_remote.rename(columns={"value": "time_ms"})
                df_remote.to_csv("runturn_remote_all.csv", index=False)
                for label, sub in df_remote.groupby("label"):
                    sub = sub.sort_values("workers")
                    sub.to_csv(f"runturn_remote_{label}.csv", index=False)
                    title = "RunTurn: Varying workers (Remote)" if label != "unknown" else "Remote"
                    save_barplot(sub, "workers", "time_ms", title, "Worker threads", f"runturn_remote_{label}.png")


if __name__ == "__main__":
    main()
