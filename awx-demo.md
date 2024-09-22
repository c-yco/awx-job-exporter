kubectl läuft nur direkt in wsl nicht im dev container

````
kubectl get secret awx-demo-admin-password -o jsonpath="{.data.password}" -n awx | base64 --decode;
````

user: admin