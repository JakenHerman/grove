"""LP relaxation of the user's ILP, solved with PuLP.

Same model; variables declared as LpContinuous instead of LpInteger.
"""

import pulp

prob = pulp.LpProblem("user_lp_relaxation", pulp.LpMaximize)

x1 = pulp.LpVariable("x1", -15, 15, pulp.LpContinuous)
x2 = pulp.LpVariable("x2", -15, 15, pulp.LpContinuous)
x3 = pulp.LpVariable("x3", -15, 15, pulp.LpContinuous)
x4 = pulp.LpVariable("x4", -15, 15, pulp.LpContinuous)
x5 = pulp.LpVariable("x5", -15, 15, pulp.LpContinuous)

prob += 2 * x1 - 3 * x2 + x3, "obj"
prob += x1 - x2 + x3 <= 5, "c1"
prob += x1 - x2 + 4 * x3 <= 7, "c2"
prob += x1 + 2 * x2 - x3 + x4 <= 14, "c3"
prob += x3 - x4 + x5 <= 7, "c4"

prob.solve(pulp.PULP_CBC_CMD(msg=False))

print(f"Status:    {pulp.LpStatus[prob.status]}")
print(f"Objective: {pulp.value(prob.objective):.2f}")
for v in (x1, x2, x3, x4, x5):
    print(f"  {v.name} = {v.varValue}")
