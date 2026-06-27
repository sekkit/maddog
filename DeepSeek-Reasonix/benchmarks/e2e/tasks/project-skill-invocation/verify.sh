set -e
got=$(tr -d '\r\n' < skill-result.txt)
want="MADDOG-BENCH:maddog|skill|invoked"
[ "$got" = "$want" ] || { echo "skill-result.txt='$got', want '$want'"; exit 1; }
