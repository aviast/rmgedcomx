import tkinter as tk
from tkinter import ttk, scrolledtext
import urllib.request
import urllib.error
import urllib.parse
import json

class GedcomXBrowserApp:
    def __init__(self, root):
        self.root = root
        self.root.title("GEDCOM X RS Hypermedia Browser")
        self.root.geometry("1150x850")

        self.headers = {'Accept': 'application/x-gedcomx-v1+json'}

        # State / History management
        self.history_stack = []
        self.forward_stack = []
        self.current_url = None
        self.current_document = {}

        # Maps treeview Item IDs to the tuple ("Type", entity_dict)
        self.loaded_entities = {}

        self.create_widgets()

    def create_widgets(self):
        # --- Top Bar (Configuration & Navigation) ---
        top_frame = ttk.Frame(self.root)
        top_frame.pack(fill=tk.X, padx=10, pady=5)

        ttk.Label(top_frame, text="Server:").pack(side=tk.LEFT, padx=5)
        self.url_entry = ttk.Entry(top_frame, width=30)
        self.url_entry.insert(0, "http://localhost:8080")
        self.url_entry.pack(side=tk.LEFT, padx=5)

        ttk.Button(top_frame, text="Connect", command=self.fetch_collections).pack(side=tk.LEFT, padx=5)

        self.collection_var = tk.StringVar()
        self.collection_combo = ttk.Combobox(top_frame, textvariable=self.collection_var, state="readonly", width=22)
        self.collection_combo.pack(side=tk.LEFT, padx=5)

        ttk.Button(top_frame, text="Start at Collection", command=self.load_selected_collection).pack(side=tk.LEFT, padx=5)

        # Navigation History Buttons
        ttk.Separator(top_frame, orient=tk.VERTICAL).pack(side=tk.LEFT, fill=tk.Y, padx=8)
        self.back_btn = ttk.Button(top_frame, text="⬅ Back", command=self.go_back, state=tk.DISABLED)
        self.back_btn.pack(side=tk.LEFT, padx=2)

        self.forward_btn = ttk.Button(top_frame, text="Forward ➡", command=self.go_forward, state=tk.DISABLED)
        self.forward_btn.pack(side=tk.LEFT, padx=2)

        # --- In-App Notification Banner ---
        self.notification_frame = tk.Frame(self.root, bg="#e8f4f8", height=28)
        self.notification_frame.pack(fill=tk.X, padx=10, pady=(2, 5))
        self.notification_frame.pack_propagate(False)

        self.notification_label = tk.Label(
            self.notification_frame,
            text="Welcome! Click 'Connect' to discover server collections.",
            bg="#e8f4f8",
            fg="#1a5276",
            font=("Arial", 10, "bold"), # FIX: Changed 9.5 to 10
            anchor="w"
        )
        self.notification_label.pack(side=tk.LEFT, padx=10, fill=tk.BOTH, expand=True)

        # --- Main Layout (PanedWindow) ---
        main_pane = ttk.PanedWindow(self.root, orient=tk.HORIZONTAL)
        main_pane.pack(fill=tk.BOTH, expand=True, padx=10, pady=5)

        # Left Pane: Entities in Current State
        left_frame = ttk.LabelFrame(main_pane, text="Entities in Current State")
        main_pane.add(left_frame, weight=1)

        self.entity_tree = ttk.Treeview(left_frame, columns=("Type", "ID", "Name"), show="headings", selectmode="browse")
        self._entity_column_labels = {"Type": "Type", "ID": "ID", "Name": "Name / Title"}
        self._entity_sort_column = None
        self._entity_sort_reverse = False
        for column, label in self._entity_column_labels.items():
            self.entity_tree.heading(
                column,
                text=label,
                command=lambda c=column: self.sort_entity_tree(c),
            )
        self.entity_tree.column("Type", width=80, stretch=False)
        self.entity_tree.column("ID", width=100, stretch=False)
        self.entity_tree.column("Name", width=180)
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

        # Right Bottom: Tabbed Details (Visual First!)
        self.details_notebook = ttk.Notebook(right_frame)
        self.details_notebook.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        # Tab 1: Visual Representation (Default)
        self.visual_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.visual_tab, text="Visual Representation")

        # Tab 2: JSON
        self.json_tab = ttk.Frame(self.details_notebook)
        self.details_notebook.add(self.json_tab, text="Details (JSON)")
        self.json_text = scrolledtext.ScrolledText(self.json_tab, wrap=tk.WORD, font=("Consolas", 10))
        self.json_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)

        # Scrollable Canvas setup for the Visual Tab
        self.setup_visual_canvas()

    def setup_visual_canvas(self):
        self.visual_canvas = tk.Canvas(self.visual_tab, bg="#f5f7fa")
        self.visual_scrollbar_y = ttk.Scrollbar(self.visual_tab, orient="vertical", command=self.visual_canvas.yview)

        self.visual_scrollable_frame = ttk.Frame(self.visual_canvas)
        self.visual_scrollable_frame.bind(
            "<Configure>",
            lambda e: self.visual_canvas.configure(scrollregion=self.visual_canvas.bbox("all"))
        )
        self.canvas_window = self.visual_canvas.create_window((0, 0), window=self.visual_scrollable_frame, anchor="nw")
        self.visual_canvas.configure(yscrollcommand=self.visual_scrollbar_y.set)

        # Keep internal frame centered horizontally when canvas resizes
        self.visual_canvas.bind('<Configure>', self._on_canvas_resize)

        self.visual_canvas.pack(side="left", fill="both", expand=True)
        self.visual_scrollbar_y.pack(side="right", fill="y")

    def _on_canvas_resize(self, event):
        canvas_width = event.width
        # Canvas window items do not support a ``minwidth`` option. Their
        # supported sizing option is ``width``; using ``minwidth`` raises a
        # TclError every time Tk emits a <Configure> event.
        self.visual_canvas.itemconfig(self.canvas_window, width=canvas_width)

    def show_notification(self, message, level="info"):
        """Updates the in-app notification banner without modal popups."""
        colors = {
            "info": ("#e8f4f8", "#1a5276"),      # Light Blue
            "success": ("#e8f8f5", "#117a65"),   # Mint Green
            "warning": ("#fef9e7", "#b7950b"),   # Light Yellow/Brown
            "error": ("#fadbd8", "#78281f")      # Light Red
        }
        bg_color, fg_color = colors.get(level, colors["info"])
        self.notification_frame.config(bg=bg_color)
        self.notification_label.config(text=message, bg=bg_color, fg=fg_color)

    # --- Discovery / Entry Point ---
    def fetch_collections(self):
        base_url = self.url_entry.get().strip().rstrip('/')
        full_url = f"{base_url}/collections"

        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                if response.status == 204:
                    self.show_notification("Server returned 204 No Content for collections.", "warning")
                    return

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
                        self.show_notification(f"Connected! {len(combo_values)} collection(s) found. Click 'Start at Collection'.", "success")
        except Exception as e:
            self.show_notification(f"Could not connect to server: {e}", "error")

    def load_selected_collection(self):
        selected = self.collection_var.get()
        if not selected or not hasattr(self, '_collection_urls'):
            self.show_notification("Please select a collection first.", "warning")
            return

        target_href = self._collection_urls.get(selected)
        if target_href:
            self.history_stack.clear()
            self.forward_stack.clear()
            self.navigate_to(target_href, navigation_mode="new")

    # --- Hypermedia Navigation System ---
    def navigate_to(self, href, navigation_mode="new"):
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
                # Handle 204 No Content
                if response.status == 204:
                    self.show_notification(f"HTTP 204 No Content: No resource exists at '{href}' (e.g., no spouses or parents linked).", "warning")
                    return

                raw_data = response.read().decode('utf-8')

                if not raw_data.strip():
                    self.show_notification(f"HTTP 204 No Content: Empty response body received for '{href}'.", "warning")
                    return

                self.current_document = json.loads(raw_data)

                self.json_text.delete(1.0, tk.END)
                self.json_text.insert(tk.END, json.dumps(self.current_document, indent=4))

                self.populate_entities()
                self.render_link_buttons(self.current_document.get('links', {}), "Entire State Document")

                self.show_notification(f"State loaded successfully from {href}", "success")

                # Default selection or prompt
                self.clear_visual_canvas()
                children_ids = self.entity_tree.get_children()
                if children_ids:
                    # Automatically select the first entity to render visually
                    self.entity_tree.selection_set(children_ids[0])
                else:
                    self.render_empty_visual_state("No entities present in this State.")

        except urllib.error.HTTPError as e:
            if e.code == 204:
                self.show_notification(f"HTTP 204 No Content: The requested relationship or entity does not exist.", "warning")
            else:
                self.show_notification(f"HTTP Error {e.code}: {e.reason}", "error")
        except Exception as e:
            self.show_notification(f"Error navigating to {href}: {e}", "error")

    def go_back(self):
        if self.history_stack:
            prev_url = self.history_stack.pop()
            self.navigate_to(prev_url, navigation_mode="back")

    def go_forward(self):
        if self.forward_stack:
            next_url = self.forward_stack.pop()
            self.navigate_to(next_url, navigation_mode="forward")

    def update_ui_state(self):
        self.back_btn.config(state=tk.NORMAL if self.history_stack else tk.DISABLED)
        self.forward_btn.config(state=tk.NORMAL if self.forward_stack else tk.DISABLED)

    def sort_entity_tree(self, column):
        """Sorts entity rows by a clicked header, toggling the direction."""
        if column == self._entity_sort_column:
            self._entity_sort_reverse = not self._entity_sort_reverse
        else:
            self._entity_sort_column = column
            self._entity_sort_reverse = False

        column_index = self.entity_tree["columns"].index(column)
        rows = [
            (self._entity_sort_key(column, self.entity_tree.item(item, "values")[column_index]), item)
            for item in self.entity_tree.get_children("")
        ]
        rows.sort(key=lambda row: row[0], reverse=self._entity_sort_reverse)
        for position, (_, item) in enumerate(rows):
            self.entity_tree.move(item, "", position)

        direction = " ▼" if self._entity_sort_reverse else " ▲"
        for name, label in self._entity_column_labels.items():
            suffix = direction if name == column else ""
            self.entity_tree.heading(name, text=label + suffix)

    @staticmethod
    def _entity_sort_key(column, value):
        text = str(value).casefold()
        if column == "ID":
            prefix = text.rstrip("0123456789")
            number = text[len(prefix):]
            if number:
                return prefix, int(number)
        return text,
    # --- UI Rendering ---
    def populate_entities(self):
        for item in self.entity_tree.get_children():
            self.entity_tree.delete(item)
        self.loaded_entities.clear()

        for person in self.current_document.get('persons', []):
            pid = person.get('id', 'Unknown')
            name = person.get('display', {}).get('name', 'Unknown Name')
            if name == 'Unknown Name':
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

    # --- Visual Pedigree Card Rendering ---
    def clear_visual_canvas(self):
        for widget in self.visual_scrollable_frame.winfo_children():
            widget.destroy()

    def render_empty_visual_state(self, message):
        center_container = ttk.Frame(self.visual_scrollable_frame)
        center_container.pack(expand=True, fill=tk.BOTH, pady=100)
        ttk.Label(center_container, text=message, font=("Arial", 12, "italic")).pack(anchor=tk.CENTER)

    def create_person_card(self, parent_widget, person_data, is_selected=False):
        """Creates a selectable rectangular card representing a Person."""
        border_color = "#2b5c8f" if is_selected else "#bdc3c7"
        bg_color = "#ebf5fb" if is_selected else "#ffffff"
        border_width = 3 if is_selected else 1

        card = tk.Frame(
            parent_widget,
            bg=bg_color,
            highlightbackground=border_color,
            highlightthickness=border_width,
            width=260,
            height=145,
            padx=12,
            pady=10,
            cursor="hand2"
        )
        # Keep every card the same size regardless of its text content.
        card.pack_propagate(False)

        display = person_data.get('display', {})
        name = display.get('name', 'Unknown Name')
        gender = display.get('gender', 'Unknown Gender')
        lifespan = display.get('lifespan', '')

        if name == 'Unknown Name':
            try: name = person_data['names'][0]['nameForms'][0]['fullText']
            except: pass

        if gender == 'Unknown Gender':
            try: gender = person_data['gender']['type'].split('/')[-1]
            except: pass

        role_text = "Active Person" if is_selected else "Person"
        lbl_role = tk.Label(card, text=role_text.upper(), font=("Arial", 8, "bold"), fg="#2b5c8f" if is_selected else "gray", bg=bg_color)
        lbl_role.pack(anchor=tk.W)

        lbl_name = tk.Label(card, text=name, font=("Arial", 12, "bold"), bg=bg_color, wraplength=230, justify=tk.LEFT)
        lbl_name.pack(anchor=tk.W, pady=(2, 0))

        clickable_widgets = [card, lbl_role, lbl_name]
        if lifespan:
            lbl_life = tk.Label(card, text=lifespan, font=("Arial", 10), fg="#16a085", bg=bg_color)
            lbl_life.pack(anchor=tk.W)
            clickable_widgets.append(lbl_life)

        lbl_gen = tk.Label(card, text=f"Gender: {gender}", font=("Arial", 9), bg=bg_color)
        lbl_gen.pack(anchor=tk.W, pady=(4, 0))

        pid = person_data.get('id', 'N/A')
        lbl_id = tk.Label(card, text=f"ID: {pid}", font=("Arial", 8), fg="gray", bg=bg_color)
        lbl_id.pack(anchor=tk.W)

        # Make the card and every visible label inside it clickable.
        clickable_widgets.extend((lbl_gen, lbl_id))
        for widget in clickable_widgets:
            widget.bind("<Button-1>", lambda e, person=person_data: self.open_person_card(person))

        return card

    def open_person_card(self, person_data):
        """Selects a visible person or navigates to a related person's state."""
        person_id = person_data.get("id")
        if self.select_person_by_id(person_id):
            return

        href = self._link_href(person_data.get("links", {}), "person")
        if href:
            self.navigate_to(href, navigation_mode="new")
        else:
            self.show_notification(f"Could not open person {person_id}: no person link was supplied.", "warning")

    def select_person_by_id(self, person_id):
        """Finds a person in the Treeview by ID and selects them."""
        for item in self.entity_tree.get_children():
            values = self.entity_tree.item(item)['values']
            if len(values) >= 2 and str(values[1]) == str(person_id):
                self.entity_tree.selection_set(item)
                self.entity_tree.see(item)
                return True
        return False
    def _link_href(self, links, relation):
        """Returns the first href for a GEDCOM X link relation, if present."""
        link = links.get(relation)
        if isinstance(link, dict):
            return link.get("href")
        if isinstance(link, list) and link and isinstance(link[0], dict):
            return link[0].get("href")
        return None

    def fetch_related_people(self, person_data, relation):
        """Loads people from a selected person's parents or children link."""
        href = self._link_href(person_data.get("links", {}), relation)
        if not href:
            return []

        base_url = self.url_entry.get().strip().rstrip('/')
        full_url = urllib.parse.urljoin(f"{base_url}/", href)
        try:
            req = urllib.request.Request(full_url, headers=self.headers)
            with urllib.request.urlopen(req) as response:
                if response.status == 204:
                    return []
                raw_data = response.read().decode("utf-8")

            if not raw_data.strip():
                return []
            document = json.loads(raw_data)
            return document.get("persons", [])
        except urllib.error.HTTPError as e:
            if e.code != 204:
                self.show_notification(
                    f"Could not load {relation} for this person: HTTP {e.code} {e.reason}",
                    "error",
                )
        except (json.JSONDecodeError, OSError, urllib.error.URLError) as e:
            self.show_notification(f"Could not load {relation} for this person: {e}", "error")
        return []
    def draw_3_generation_view(self, selected_person):
        """Draws Parents, Selected Person, and Children in a centered 3-generation layout."""
        self.clear_visual_canvas()

        # The collection/person document only contains the selected person.
        # Related people and their ParentChild relationships are exposed through
        # the person's hypermedia links, so load those state documents directly.
        parents = self.fetch_related_people(selected_person, "parents")
        children = self.fetch_related_people(selected_person, "children")

        # Master Outer Frame centered inside Visual Canvas
        outer_container = ttk.Frame(self.visual_scrollable_frame)
        outer_container.pack(expand=True, fill=tk.BOTH, padx=20, pady=20)

        # --- Generation 1: PARENTS (Top Row) ---
        gen1_frame = ttk.LabelFrame(outer_container, text="Parents", padding=10)
        gen1_frame.pack(fill=tk.X, pady=(0, 10))

        parents_inner = ttk.Frame(gen1_frame)
        parents_inner.pack(anchor=tk.CENTER)

        if parents:
            for p in parents:
                card = self.create_person_card(parents_inner, p, is_selected=False)
                card.pack(side=tk.LEFT, padx=10, pady=5)
        else:
            ttk.Label(parents_inner, text="No known parents for this person.", font=("Arial", 9, "italic")).pack(pady=5)

        # Connector Indicator
        ttk.Label(outer_container, text="│\n▼", font=("Arial", 12, "bold"), foreground="gray").pack(anchor=tk.CENTER)

        # --- Generation 2: SELECTED PERSON (Middle Row) ---
        gen2_frame = ttk.Frame(outer_container)
        gen2_frame.pack(fill=tk.X, pady=5)

        main_card = self.create_person_card(gen2_frame, selected_person, is_selected=True)
        main_card.pack(anchor=tk.CENTER, pady=5)

        # Connector Indicator
        ttk.Label(outer_container, text="│\n▼", font=("Arial", 12, "bold"), foreground="gray").pack(anchor=tk.CENTER)

        # --- Generation 3: CHILDREN (Bottom Row with Horizontal Scrollbar) ---
        gen3_frame = ttk.LabelFrame(outer_container, text=f"Children ({len(children)})", padding=10)
        gen3_frame.pack(fill=tk.X, pady=(10, 0))

        if children:
            # Setup horizontal scrollable area for children
            child_canvas = tk.Canvas(gen3_frame, height=170, bg="#f5f7fa", highlightthickness=0)
            child_hscrollbar = ttk.Scrollbar(gen3_frame, orient="horizontal", command=child_canvas.xview)

            child_inner_frame = ttk.Frame(child_canvas)
            child_inner_frame.bind(
                "<Configure>",
                lambda e: child_canvas.configure(scrollregion=child_canvas.bbox("all"))
            )

            child_canvas.create_window((0, 0), window=child_inner_frame, anchor="nw")
            child_canvas.configure(xscrollcommand=child_hscrollbar.set)

            child_canvas.pack(fill=tk.X, expand=True)
            child_hscrollbar.pack(fill=tk.X)

            for c in children:
                card = self.create_person_card(child_inner_frame, c, is_selected=False)
                card.pack(side=tk.LEFT, padx=10, pady=5)
        else:
            ttk.Label(gen3_frame, text="No known children for this person.", font=("Arial", 9, "italic")).pack(anchor=tk.CENTER, pady=5)

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
            if entity_type == "Person":
                self.draw_3_generation_view(entity_data)
            else:
                self.clear_visual_canvas()
                self.render_empty_visual_state(f"Visual 3-generation representation is available for Persons.\n(Selected entity is a {entity_type}).")


if __name__ == "__main__":
    root = tk.Tk()
    app = GedcomXBrowserApp(root)
    root.mainloop()
