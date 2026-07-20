import tkinter as tk
from tkinter import ttk, scrolledtext, messagebox
import urllib.request
import urllib.error
import urllib.parse
import json

class GedcomXBrowserApp:
    def __init__(self, root):
        self.root = root
        self.root.title("GEDCOM X RS Hypermedia Browser")
        self.root.geometry("1100x800")

        self.headers = {'Accept': 'application/x-gedcomx-v1+json'}

        # State / History management
        self.history_stack = []
        self.forward_stack = []
        self.current_url = None
        self.current_document = {}

        # Maps treeview Item IDs to the JSON dictionary of the entity
        self.loaded_entities = {}

        self.create_widgets()

    def create_widgets(self):
        # --- Top Bar (Configuration & Navigation) ---
        top_frame = ttk.Frame(self.root)
        top_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(top_frame, text="Server:").pack(side=tk.LEFT, padx=5)
        self.url_entry = ttk.Entry(top_frame, width=35)
        self.url_entry.insert(0, "http://localhost:8080")
        self.url_entry.pack(side=tk.LEFT, padx=5)

        ttk.Button(top_frame, text="Connect", command=self.fetch_collections).pack(side=tk.LEFT, padx=5)

        self.collection_var = tk.StringVar()
        self.collection_combo = ttk.Combobox(top_frame, textvariable=self.collection_var, state="readonly", width=25)
        self.collection_combo.pack(side=tk.LEFT, padx=5)

        ttk.Button(top_frame, text="Start at Collection", command=self.load_selected_collection).pack(side=tk.LEFT, padx=5)

        # Navigation History
        ttk.Separator(top_frame, orient=tk.VERTICAL).pack(side=tk.LEFT, fill=tk.Y, padx=10)
        self.back_btn = ttk.Button(top_frame, text="⬅ Back", command=self.go_back, state=tk.DISABLED)
        self.back_btn.pack(side=tk.LEFT, padx=2)

        self.forward_btn = ttk.Button(top_frame, text="Forward ➡", command=self.go_forward, state=tk.DISABLED)
        self.forward_btn.pack(side=tk.LEFT, padx=2)

        # Current State (URL) display
        self.status_var = tk.StringVar(value="State: Ready")
        ttk.Label(top_frame, textvariable=self.status_var, foreground="gray").pack(side=tk.LEFT, padx=10)

        # --- Main Layout (PanedWindow) ---
        main_pane = ttk.PanedWindow(self.root, orient=tk.HORIZONTAL)
        main_pane.pack(fill=tk.BOTH, expand=True, padx=10, pady=5)

        # Left Pane: Entities in Current State
        left_frame = ttk.LabelFrame(main_pane, text="Entities in Current State")
        main_pane.add(left_frame, weight=1)

        self.entity_tree = ttk.Treeview(left_frame, columns=("Type", "ID", "Name"), show="headings", selectmode="browse")
        self.entity_tree.heading("Type", text="Type")
        self.entity_tree.heading("ID", text="ID")
        self.entity_tree.heading("Name", text="Name / Title")
        self.entity_tree.column("Type", width=100, stretch=False)
        self.entity_tree.column("ID", width=120, stretch=False)
        self.entity_tree.column("Name", width=200)
        self.entity_tree.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)
        self.entity_tree.bind("<<TreeviewSelect>>", self.on_entity_select)

        # Right Pane: Links & Data
        right_frame = ttk.Frame(main_pane)
        main_pane.add(right_frame, weight=3)

        # Right Top: Dynamic Link Buttons
        self.links_frame = ttk.LabelFrame(right_frame, text="Available State Transitions (Links)")
        self.links_frame.pack(fill=tk.X, padx=5, pady=5)

        self.links_inner_frame = ttk.Frame(self.links_frame)
        self.links_inner_frame.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        # Right Bottom: Tabbed Details
        self.details_notebook = ttk.Notebook(right_frame)
        self.details_notebook.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        # Tab 1: JSON
        self.json_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.json_tab, text="Details (JSON)")
        self.json_text = scrolledtext.ScrolledText(self.json_tab, wrap=tk.WORD, font=("Consolas", 10))
        self.json_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        # Tab 2: Visual
        self.visual_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.visual_tab, text="Visual Representation")

        # Setting up a scrollable canvas for the Visual tab
        self.visual_canvas = tk.Canvas(self.visual_tab, bg="#f0f0f0")
        self.visual_scrollbar = ttk.Scrollbar(self.visual_tab, orient="vertical", command=self.visual_canvas.yview)
        self.visual_scrollable_frame = ttk.Frame(self.visual_canvas)

        self.visual_scrollable_frame.bind(
            "<Configure>",
            lambda e: self.visual_canvas.configure(scrollregion=self.visual_canvas.bbox("all"))
        )
        self.visual_canvas.create_window((0, 0), window=self.visual_scrollable_frame, anchor="nw")
        self.visual_canvas.configure(yscrollcommand=self.visual_scrollbar.set)

        self.visual_canvas.pack(side="left", fill="both", expand=True)
        self.visual_scrollbar.pack(side="right", fill="y")

    # --- Discovery / Entry Point ---
    def fetch_collections(self):
        base_url = self.url_entry.get().strip().rstrip('/')
        full_url = f"{base_url}/collections"

        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                parsed_json = json.loads(response.read().decode('utf-8'))

                if 'collections' in parsed_json:
                    combo_values = []
                    self._collection_urls = {}
                    for col in parsed_json['collections']:
                        col_id = col.get('id', 'Unknown')
                        display_name = f"{col.get('title', 'Unnamed')} ({col_id})"

                        self_href = None
                        links = col.get('links', {})
                        if 'self' in links:
                            self_href = links['self'].get('href')
                        if not self_href:
                            self_href = f"/collections/{col_id}"

                        self._collection_urls[display_name] = self_href
                        combo_values.append(display_name)

                    self.collection_combo['values'] = combo_values
                    if combo_values:
                        self.collection_combo.current(0)
                        messagebox.showinfo("Connected", "Collections loaded. Select one and click 'Start at Collection'.")
        except Exception as e:
            messagebox.showerror("Error", f"Could not fetch collections:\n{e}")

    def load_selected_collection(self):
        selected = self.collection_var.get()
        if not selected or not hasattr(self, '_collection_urls'):
            return

        target_href = self._collection_urls.get(selected)
        if target_href:
            self.navigate_to(target_href, navigation_mode="new")

    # --- Hypermedia Navigation System ---
    def navigate_to(self, href, navigation_mode="new"):
        """
        navigation_mode can be: 'new', 'back', or 'forward'.
        """
        base_url = self.url_entry.get().strip().rstrip('/')
        full_url = urllib.parse.urljoin(f"{base_url}/", href)

        if self.current_url:
            if navigation_mode == "new":
                self.history_stack.append(self.current_url)
                self.forward_stack.clear()
            elif navigation_mode == "back":
                self.forward_stack.append(self.current_url)
            elif navigation_mode == "forward":
                self.history_stack.append(self.current_url)

        self.current_url = full_url
        self.update_ui_state()

        self.json_text.delete(1.0, tk.END)
        self.json_text.insert(tk.END, f"GET {full_url}...\n\n")
        self.root.update()

        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                raw_data = response.read().decode('utf-8')
                self.current_document = json.loads(raw_data)

                self.json_text.delete(1.0, tk.END)
                self.json_text.insert(tk.END, json.dumps(self.current_document, indent=4))

                self.populate_entities()
                self.render_link_buttons(self.current_document.get('links', {}), "Entire State Document")

                # Clear the visual state until an entity is specifically selected
                self.clear_visual_canvas()
                ttk.Label(self.visual_scrollable_frame, text="Select an entity from the list to view its visual state.", font=("Arial", 12, "italic")).pack(pady=20, padx=20)

        except urllib.error.HTTPError as e:
            self.json_text.insert(tk.END, f"HTTP Error {e.code}: {e.reason}\n")
        except Exception as e:
            self.json_text.insert(tk.END, f"Error:\n{e}")

    def go_back(self):
        if self.history_stack:
            prev_url = self.history_stack.pop()
            self.navigate_to(prev_url, navigation_mode="back")

    def go_forward(self):
        if self.forward_stack:
            next_url = self.forward_stack.pop()
            self.navigate_to(next_url, navigation_mode="forward")

    def update_ui_state(self):
        self.status_var.set(f"State: {self.current_url}")
        self.back_btn.config(state=tk.NORMAL if self.history_stack else tk.DISABLED)
        self.forward_btn.config(state=tk.NORMAL if self.forward_stack else tk.DISABLED)

    # --- UI Rendering ---
    def populate_entities(self):
        for item in self.entity_tree.get_children():
            self.entity_tree.delete(item)
        self.loaded_entities.clear()

        # Extract standard entities
        for person in self.current_document.get('persons', []):
            pid = person.get('id', 'Unknown')
            name = person.get('display', {}).get('name', 'Unknown Name')
            if name == 'Unknown Name': # Fallback
                try: name = person['names'][0]['nameForms'][0]['fullText']
                except: pass
            tree_id = self.entity_tree.insert("", tk.END, values=("Person", pid, name))
            self.loaded_entities[tree_id] = ("Person", person)

        for place in self.current_document.get('places', []):
            pid = place.get('id', 'Unknown')
            name = "Unknown Place"
            try: name = place['names'][0]['value']
            except: pass
            tree_id = self.entity_tree.insert("", tk.END, values=("Place", pid, name))
            self.loaded_entities[tree_id] = ("Place", place)

        for src in self.current_document.get('sourceDescriptions', []):
            sid = src.get('id', 'Unknown')
            title = "Unknown Source"
            try: title = src['titles'][0]['value']
            except: pass
            tree_id = self.entity_tree.insert("", tk.END, values=("Source", sid, title))
            self.loaded_entities[tree_id] = ("Source", src)

    def render_link_buttons(self, links_dict, context_label):
        self.links_frame.config(text=f"Available State Transitions ({context_label})")
        for widget in self.links_inner_frame.winfo_children():
            widget.destroy()

        if not links_dict:
            ttk.Label(self.links_inner_frame, text="No links available.", font=("", 10, "italic")).pack(side=tk.LEFT)
            return

        for rel, link_data in links_dict.items():
            if isinstance(link_data, dict): href = link_data.get('href')
            elif isinstance(link_data, list) and len(link_data) > 0: href = link_data[0].get('href')
            else: continue

            if href:
                btn = ttk.Button(self.links_inner_frame, text=rel, command=lambda h=href: self.navigate_to(h, navigation_mode="new"))
                btn.pack(side=tk.LEFT, padx=2, pady=2)

    # --- Visual Card Rendering ---
    def clear_visual_canvas(self):
        for widget in self.visual_scrollable_frame.winfo_children():
            widget.destroy()

    def draw_person_card(self, person_data):
        """Draws a rectangular card representing a Person."""
        card = tk.Frame(self.visual_scrollable_frame, bg="white", highlightbackground="black", highlightthickness=2, padx=15, pady=15)
        card.pack(anchor=tk.NW, padx=20, pady=20, fill=tk.X)

        # Try to pull from GEDCOM X Display properties first
        display = person_data.get('display', {})

        name = display.get('name', 'Unknown Name')
        gender = display.get('gender', 'Unknown Gender')
        lifespan = display.get('lifespan', '')

        # Fallbacks if display properties are missing
        if name == 'Unknown Name':
            try: name = person_data['names'][0]['nameForms'][0]['fullText']
            except: pass

        if gender == 'Unknown Gender':
            try: gender = person_data['gender']['type'].split('/')[-1] # Extract 'Male' from 'http://gedcomx.org/Male'
            except: pass

        # Draw the layout inside the rectangle
        tk.Label(card, text="Person", font=("Arial", 10, "bold"), fg="gray", bg="white").pack(anchor=tk.W)
        tk.Label(card, text=name, font=("Arial", 18, "bold"), bg="white").pack(anchor=tk.W, pady=(5, 0))

        if lifespan:
            tk.Label(card, text=lifespan, font=("Arial", 12), fg="blue", bg="white").pack(anchor=tk.W)

        tk.Label(card, text=f"Gender: {gender}", font=("Arial", 12), bg="white").pack(anchor=tk.W, pady=(10, 0))
        tk.Label(card, text=f"ID: {person_data.get('id', 'N/A')}", font=("Arial", 9), fg="gray", bg="white").pack(anchor=tk.W, pady=(5, 0))

    # --- Interaction Events ---
    def on_entity_select(self, event):
        selection = self.entity_tree.selection()
        if not selection:
            return

        tree_id = selection[0]
        entity_tuple = self.loaded_entities.get(tree_id)

        if entity_tuple:
            entity_type, entity_data = entity_tuple

            # 1. Update JSON pane
            self.json_text.delete(1.0, tk.END)
            self.json_text.insert(tk.END, json.dumps(entity_data, indent=4))

            # 2. Update Links pane
            entity_name = self.entity_tree.item(tree_id)['values'][2]
            entity_links = entity_data.get('links', {})
            self.render_link_buttons(entity_links, f"Selected: {entity_name}")

            # 3. Update Visual State pane
            self.clear_visual_canvas()
            if entity_type == "Person":
                self.draw_person_card(entity_data)
            else:
                # Basic placeholder for Places and Sources until we iterate on them
                ttk.Label(self.visual_scrollable_frame, text=f"Visual representation for {entity_type} coming soon.", font=("Arial", 12)).pack(pady=20, padx=20)


if __name__ == "__main__":
    root = tk.Tk()
    app = GedcomXBrowserApp(root)
    root.mainloop()
