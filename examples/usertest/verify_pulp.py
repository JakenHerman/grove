"""Cross-check the grove result on the user's ILP using PuLP.

    max   2 x1 - 3 x2 + x3
    s.t.  x1 - x2 +  x3            <= 5
          x1 - x2 + 4 x3           <= 7
          x1 + 2 x2 -  x3 + x4     <= 14
                        x3 - x4 + x5 <= 7
          x1..x5 in [-15, 15], integer
"""

import pulp

def solve(kind):
    prob = pulp.LpProblem("user_ilp", pulp.LpMaximize)
    cat = pulp.LpInteger if kind == "integer" else pulp.LpContinuous
    x1 = pulp.LpVariable("x1", -15, 15, cat)
    x2 = pulp.LpVariable("x2", -15, 15, cat)
    x3 = pulp.LpVariable("x3", -15, 15, cat)
    x4 = pulp.LpVariable("x4", -15, 15, cat)
    x5 = pulp.LpVariable("x5", -15, 15, cat)

    prob += 2 * x1 - 3 * x2 + x3, "obj"
    prob += x1 - x2 + x3 <= 5, "c1"
    prob += x1 - x2 + 4 * x3 <= 7, "c2"
    prob += x1 + 2 * x2 - x3 + x4 <= 14, "c3"
    prob += x3 - x4 + x5 <= 7, "c4"

    # CBC is the default bundled solver; msg=False suppresses the banner.
    prob.solve(pulp.PULP_CBC_CMD(msg=False))

    print(f"--- {kind} ---")
    print(f"Status:    {pulp.LpStatus[prob.status]}")
    print(f"Objective: {pulp.value(prob.objective):.2f}")
    for v in (x1, x2, x3, x4, x5):
        print(f"  {v.name} = {v.varValue}")
    print()


if __name__ == "__main__":
    solve("integer")
    solve("continuous")
