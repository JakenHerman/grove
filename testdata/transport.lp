\ Classical transportation LP: ship units from two plants to three
\ warehouses at minimum total cost, subject to supply caps and demand
\ floors. Small enough to eyeball, large enough to exercise multiple
\ sections and a variety of bound forms.

Minimize
 cost: 4 x11 + 6 x12 + 9 x13
     + 5 x21 + 3 x22 + 8 x23

Subject To
 supply_1: x11 + x12 + x13 <= 100
 supply_2: x21 + x22 + x23 <= 150
 demand_1: x11 + x21 >= 80
 demand_2: x12 + x22 >= 70
 demand_3: x13 + x23 >= 100

Bounds
 0 <= x11 <= 100
 0 <= x12 <= 100
 0 <= x13 <= 100
 0 <= x21 <= 150
 0 <= x22 <= 150
 0 <= x23 <= 150

End
