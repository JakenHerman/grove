"""Second user problem:

    min   2 x1 - 3 x2 + x3
    s.t.  x1 - x2 >= 0.50
          x1 - x2 <= 0.75
          x2 - x3 <= 1.25
          x2 - x3 >= 0.95
          x1, x2, x3 in [-1, 1]

Run once as ILP (LpInteger) and once as LP relaxation (LpContinuous).
"""

import pulp

def solve(label, cat):
    prob = pulp.LpProblem("user2_" + label, pulp.LpMinimize)
    x1 = pulp.LpVariable("x1", -1, 1, cat)
    x2 = pulp.LpVariable("x2", -1, 1, cat)
    x3 = pulp.LpVariable("x3", -1, 1, cat)

    prob += 2 * x1 - 3 * x2 + x3, "obj"
    prob += x1 - x2 >= 0.50, "c1"
    prob += x1 - x2 <= 0.75, "c2"
    prob += x2 - x3 <= 1.25, "c3"
    prob += x2 - x3 >= 0.95, "c4"

    prob.solve(pulp.PULP_CBC_CMD(msg=False))

    print(f"--- {label} ---")
    print(f"Status:    {pulp.LpStatus[prob.status]}")
    if prob.status == 1:
        print(f"Objective: {pulp.value(prob.objective):.4f}")
        for v in (x1, x2, x3):
            print(f"  {v.name} = {v.varValue}")
    print()


if __name__ == "__main__":
    solve("ILP", pulp.LpInteger)
    solve("LP-relaxation", pulp.LpContinuous)
