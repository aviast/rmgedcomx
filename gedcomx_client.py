import tkinter as tk
from tkinter import ttk, scrolledtext, messagebox
import urllib.request
import urllib.error
import urllib.parse
import json

class GedcomXClientApp:
    def __init__(self, root):
        self.root = root
        self.root.title("GEDCOM X RS Client (HATEOAS)")
        self.root.geometry("1000x750")

        # GEDCOM X RS Standard Accept Header
        self.headers = {
            'Accept': 'application/x-gedcomx-v1+json'
        }

        # Store discovered URLs
        self.discovered_urls = {}

        self.create_widgets()

    def create_widgets(self):
        # --- Top Frame for Configuration ---
        config_frame = ttk.LabelFrame(self.root, text="Server Configuration")
        config_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(config_frame, text="Server Base URL:").pack(side=tk.LEFT, padx=5, pady=5)
        self.url_entry = ttk.Entry(config_frame, width=50)
        self.url_entry.insert(0, "http://localhost:8080")
        self.url_entry.pack(side=tk.LEFT, padx=5, pady=5)

        ttk.Button(config_frame, text="Connect & Load Entities", command=self.load_all_entities).pack(side=tk.LEFT, padx=10, pady=5)

        # --- Middle: Notebook for Tabs ---
        self.notebook = ttk.Notebook(self.root)
        self.notebook.pack(fill=tk.BOTH, expand=True, padx=10, pady=5)

        # Create Tabs
        self.create_person_tab()
        self.create_place_tab()
        self.create_source_tab()

    def create_person_tab(self):
        self.person_tab = ttk.Frame(self.notebook)
        self.notebook.add(self.person_tab, text="Person")

        list_frame = ttk.LabelFrame(self.person_tab, text="Persons")
        list_frame.pack(fill=tk.X, padx=5, pady=5)

        self.person_tree = ttk.Treeview(list_frame, columns=("ID", "Name"), show="headings", height=5)
        self.person_tree.heading("ID", text="Person ID")
        self.person_tree.heading("Name", text="Name")
        self.person_tree.column("ID", width=150)
        self.person_tree.column("Name", width=600)
        self.person_tree.pack(fill=tk.X, padx=5, pady=5)
        self.person_tree.bind("<<TreeviewSelect>>", self.on_person_select)

        query_frame = ttk.LabelFrame(self.person_tab, text="Queries")
        query_frame.pack(fill=tk.X, padx=5, pady=5)

        ttk.Label(query_frame, text="Person ID:").pack(side=tk.LEFT, padx=5, pady=5)
        self.person_id_var = tk.StringVar()
        self.person_id_entry = ttk.Entry(query_frame, textvariable=self.person_id_var, width=20, state='readonly')
        self.person_id_entry.pack(side=tk.LEFT, padx=5, pady=5)

        # Note: A strictly HATEOAS client would also read standard links off the Person resource to find these URLs,
        # but standard REST conventions are used here as fallbacks for the sub-resources.
        ttk.Button(query_frame, text="Parents", command=lambda: self.fetch_data(f"/persons/{self.person_id_var.get()}/parents", self.person_result_text)).pack(side=tk.LEFT, padx=2)
        ttk.Button(query_frame, text="Children", command=lambda: self.fetch_data(f"/persons/{self.person_id_var.get()}/children", self.person_result_text)).pack(side=tk.LEFT, padx=2)
        ttk.Button(query_frame, text="Spouses", command=lambda: self.fetch_data(f"/persons/{self.person_id_var.get()}/spouses", self.person_result_text)).pack(side=tk.LEFT, padx=2)
        ttk.Button(query_frame, text="Ancestry", command=lambda: self.fetch_data(f"/persons/{self.person_id_var.get()}/ancestry", self.person_result_text)).pack(side=tk.LEFT, padx=2)
        ttk.Button(query_frame, text="Descendancy", command=lambda: self.fetch_data(f"/persons/{self.person_id_var.get()}/descendancy", self.person_result_text)).pack(side=tk.LEFT, padx=2)

        result_frame = ttk.LabelFrame(self.person_tab, text="JSON Response")
        result_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)
        self.person_result_text = scrolledtext.ScrolledText(result_frame, wrap=tk.WORD, font=("Consolas", 10))
        self.person_result_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

    def create_place_tab(self):
        self.place_tab = ttk.Frame(self.notebook)
        self.notebook.add(self.place_tab, text="Place")

        list_frame = ttk.LabelFrame(self.place_tab, text="Places")
        list_frame.pack(fill=tk.X, padx=5, pady=5)

        self.place_tree = ttk.Treeview(list_frame, columns=("ID", "Name"), show="headings", height=5)
        self.place_tree.heading("ID", text="Place ID")
        self.place_tree.heading("Name", text="Name")
        self.place_tree.column("ID", width=150)
        self.place_tree.column("Name", width=600)
        self.place_tree.pack(fill=tk.X, padx=5, pady=5)
        self.place_tree.bind("<<TreeviewSelect>>", self.on_place_select)

        result_frame = ttk.LabelFrame(self.place_tab, text="JSON Response")
        result_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)
        self.place_result_text = scrolledtext.ScrolledText(result_frame, wrap=tk.WORD, font=("Consolas", 10))
        self.place_result_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

    def create_source_tab(self):
        self.source_tab = ttk.Frame(self.notebook)
        self.notebook.add(self.source_tab, text="Source Description")

        list_frame = ttk.LabelFrame(self.source_tab, text="Source Descriptions")
        list_frame.pack(fill=tk.X, padx=5, pady=5)

        self.source_tree = ttk.Treeview(list_frame, columns=("ID", "Title"), show="headings", height=5)
        self.source_tree.heading("ID", text="Source ID")
        self.source_tree.heading("Title", text="Title")
        self.source_tree.column("ID", width=150)
        self.source_tree.column("Title", width=600)
        self.source_tree.pack(fill=tk.X, padx=5, pady=5)
        self.source_tree.bind("<<TreeviewSelect>>", self.on_source_select)

        result_frame = ttk.LabelFrame(self.source_tab, text="JSON Response")
        result_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)
        self.source_result_text = scrolledtext.ScrolledText(result_frame, wrap=tk.WORD, font=("Consolas", 10))
        self.source_result_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

    # --- Event Handlers ---
    def on_person_select(self, event):
        selected = self.person_tree.selection()
        if selected:
            person_id = self.person_tree.item(selected[0])['values'][0]
            self.person_id_var.set(person_id)
            self.fetch_data(f"/persons/{person_id}", self.person_result_text)

    def on_place_select(self, event):
        selected = self.place_tree.selection()
        if selected:
            place_id = self.place_tree.item(selected[0])['values'][0]
            self.fetch_data(f"/places/{place_id}", self.place_result_text)

    def on_source_select(self, event):
        selected = self.source_tree.selection()
        if selected:
            source_id = self.source_tree.item(selected[0])['values'][0]
            self.fetch_data(f"/source-descriptions/{source_id}", self.source_result_text)

    # --- Data Fetching & HATEOAS Discovery ---
    def load_all_entities(self):
        """Discovers endpoints via the Collections root, then fetches the entities."""
        self.person_tree.delete(*self.person_tree.get_children())
        self.place_tree.delete(*self.place_tree.get_children())
        self.source_tree.delete(*self.source_tree.get_children())
        self.discovered_urls.clear()

        base_url = self.url_entry.get().strip().rstrip('/')

        # 1. Fetch Collections to discover links
        collections_data = self.make_request("/collections", self.person_result_text, is_absolute=False)

        if collections_data and 'collections' in collections_data:
            for collection in collections_data['collections']:
                if 'links' in collection:
                    for link in collection['links']:
                        rel = link.get('rel')
                        href = link.get('href')
                        if rel and href:
                            # Safely combine the base URL with the href in case the server returns a relative path
                            self.discovered_urls[rel] = urllib.parse.urljoin(f"{base_url}/", href)

        # Check what we discovered
        msg = "Discovery Complete:\n"
        for rel in ["persons", "places", "source-descriptions"]:
            msg += f"- {rel}: {self.discovered_urls.get(rel, 'Not Found')}\n"

        # 2. Load Persons
        if 'persons' in self.discovered_urls:
            person_data = self.make_request(self.discovered_urls['persons'], self.person_result_text, is_absolute=True)
            if person_data and 'persons' in person_data:
                for p in person_data['persons']:
                    pid = p.get('id', 'Unknown')
                    try:
                        name = p['names'][0]['nameForms'][0]['fullText']
                    except (KeyError, IndexError):
                        name = "Unknown Name"
                    self.person_tree.insert("", tk.END, values=(pid, name))

        # 3. Load Places
        if 'places' in self.discovered_urls:
            place_data = self.make_request(self.discovered_urls['places'], self.place_result_text, is_absolute=True)
            if place_data and 'places' in place_data:
                for pl in place_data['places']:
                    pl_id = pl.get('id', 'Unknown')
                    try:
                        name = pl['names'][0]['value']
                    except (KeyError, IndexError):
                        name = "Unknown Place Name"
                    self.place_tree.insert("", tk.END, values=(pl_id, name))

        # 4. Load Source Descriptions
        if 'source-descriptions' in self.discovered_urls:
            source_data = self.make_request(self.discovered_urls['source-descriptions'], self.source_result_text, is_absolute=True)
            if source_data and 'sourceDescriptions' in source_data:
                for src in source_data['sourceDescriptions']:
                    src_id = src.get('id', 'Unknown')
                    try:
                        title = src['titles'][0]['value']
                    except (KeyError, IndexError):
                        title = "Unknown Source Title"
                    self.source_tree.insert("", tk.END, values=(src_id, title))

        messagebox.showinfo("Load Complete", msg)

    def make_request(self, path_or_url, text_widget, is_absolute=False):
        """Helper to fetch data, handling both relative paths and absolute URLs discovered via links."""
        if is_absolute:
            full_url = path_or_url
        else:
            base_url = self.url_entry.get().strip().rstrip('/')
            full_url = f"{base_url}{path_or_url}"

        text_widget.delete(1.0, tk.END)
        text_widget.insert(tk.END, f"Fetching data from: {full_url}\n")
        text_widget.insert(tk.END, "-"*50 + "\n")
        self.root.update()

        req = urllib.request.Request(full_url, headers=self.headers)

        try:
            with urllib.request.urlopen(req) as response:
                status = response.getcode()
                raw_data = response.read().decode('utf-8')
                text_widget.insert(tk.END, f"Status Code: {status} OK\n\n")

                try:
                    parsed_json = json.loads(raw_data)
                    text_widget.insert(tk.END, json.dumps(parsed_json, indent=4))
                    return parsed_json
                except json.JSONDecodeError:
                    text_widget.insert(tk.END, raw_data)
                    return None

        except urllib.error.HTTPError as e:
            text_widget.insert(tk.END, f"HTTP Error: {e.code} {e.reason}\n\n")
            if e.code == 404:
                text_widget.insert(tk.END, "Message: Resource not found.")
            elif e.code in (405, 501):
                text_widget.insert(tk.END, "Message: Feature not implemented by server.")
            else:
                text_widget.insert(tk.END, f"Details: {e.read().decode('utf-8', errors='ignore')}")
            return None

        except urllib.error.URLError as e:
            text_widget.insert(tk.END, f"Connection Error: Failed to reach the server.\nReason: {e.reason}")
            return None
        except Exception as e:
            text_widget.insert(tk.END, f"An unexpected error occurred:\n{str(e)}")
            return None

    def fetch_data(self, endpoint, text_widget):
        if not endpoint or "None" in endpoint or endpoint.endswith("//"):
            messagebox.showwarning("Input Error", "Please select a valid entity first.")
            return

        self.make_request(endpoint, text_widget, is_absolute=False)


if __name__ == "__main__":
    root = tk.Tk()
    app = GedcomXClientApp(root)
    root.mainloop()
